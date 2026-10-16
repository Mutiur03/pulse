package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/artifacts"
	"github.com/Mutiur03/pulse/internal/audit"
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/sourcemap"
	"github.com/Mutiur03/pulse/internal/store/ch"
	"github.com/Mutiur03/pulse/internal/symbolicate"
)

// ReleaseStore keeps releases, their source map artifacts and upload tokens.
type ReleaseStore interface {
	ListReleases(ctx context.Context, project int64, limit int) ([]artifacts.Release, error)
	GetRelease(ctx context.Context, project int64, version string) (artifacts.Release, []artifacts.Artifact, error)
	EnsureRelease(ctx context.Context, project int64, version string) (artifacts.Release, error)
	PutArtifact(ctx context.Context, a artifacts.Artifact) (artifacts.Artifact, error)
	CreateToken(ctx context.Context, project int64, name string, by int64) (artifacts.Token, string, error)
	ListTokens(ctx context.Context, project int64) ([]artifacts.Token, error)
	RevokeToken(ctx context.Context, project, id int64) (artifacts.Token, error)
	Authenticate(ctx context.Context, secret string) (int64, error)
}

// ObjectPutter stores uploaded source maps.
type ObjectPutter interface {
	Put(ctx context.Context, key string, data []byte) error
}

// Upload limits for one request and for each map in it.
const (
	MaxUploadBytes   = 50 << 20
	MaxSourcemapSize = 20 << 20
	MaxUploadFiles   = 200
)

func (s *Server) releasesOK(w http.ResponseWriter) bool {
	if s.Releases == nil {
		writeErr(w, http.StatusNotFound, "releases are not enabled")
		return false
	}
	return true
}

func toRelease(r artifacts.Release) gen.Release {
	return gen.Release{Id: r.ID, Version: r.Version, CreatedAt: r.CreatedAt, ArtifactCount: int(r.Artifacts)}
}

func toArtifact(a artifacts.Artifact) gen.Artifact {
	return gen.Artifact{Id: a.ID, Name: a.Name, DebugId: strPtr(a.DebugID), Size: a.Size, Sha256: a.SHA256, CreatedAt: a.CreatedAt}
}

func toToken(t artifacts.Token) gen.UploadToken {
	return gen.UploadToken{Id: t.ID, Name: t.Name, Hint: t.Hint, CreatedAt: t.CreatedAt, LastUsedAt: t.LastUsedAt, RevokedAt: t.RevokedAt}
}

func (s *Server) ListReleases(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath, params gen.ListReleasesParams) {
	if !canRead(r, projectID) {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	if !s.releasesOK(w) {
		return
	}
	limit := 50
	if params.Limit != nil && *params.Limit >= 1 && *params.Limit <= 200 {
		limit = *params.Limit
	}
	list, err := s.Releases.ListReleases(r.Context(), projectID, limit)
	if err != nil {
		internal(w, err)
		return
	}
	out := make([]gen.Release, 0, len(list))
	for _, rel := range list {
		out = append(out, toRelease(rel))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) GetRelease(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath, version gen.ReleasePath) {
	if !canRead(r, projectID) {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	if !s.releasesOK(w) {
		return
	}
	rel, arts, err := s.Releases.GetRelease(r.Context(), projectID, version)
	if errors.Is(err, artifacts.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "release not found")
		return
	} else if err != nil {
		internal(w, err)
		return
	}
	out := gen.ReleaseDetail{Release: toRelease(rel), Artifacts: make([]gen.Artifact, 0, len(arts))}
	for _, a := range arts {
		out.Artifacts = append(out.Artifacts, toArtifact(a))
	}
	writeJSON(w, http.StatusOK, out)
}

// UploadSourcemaps stores each multipart file as a source map of the release.
// It authenticates with an upload token only; see auth.Middleware.
func (s *Server) UploadSourcemaps(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath, version gen.ReleasePath) {
	if !s.releasesOK(w) {
		return
	}
	if s.ArtifactObjects == nil {
		writeErr(w, http.StatusServiceUnavailable, "object storage is not configured")
		return
	}
	secret, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	project, err := s.Releases.Authenticate(r.Context(), strings.TrimSpace(secret))
	if errors.Is(err, artifacts.ErrNotFound) || (err == nil && project != projectID) {
		writeErr(w, http.StatusUnauthorized, "invalid upload token")
		return
	} else if err != nil {
		internal(w, err)
		return
	}
	if !artifacts.ValidVersion(version) {
		writeErr(w, http.StatusBadRequest, "release may use letters, digits and . _ - + @, up to 200 characters")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadBytes)
	if err := r.ParseMultipartForm(8 << 20); err != nil { //nolint:gosec // body capped by MaxBytesReader above
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeErr(w, http.StatusRequestEntityTooLarge, "upload is larger than 50 MB")
			return
		}
		writeErr(w, http.StatusBadRequest, "expected a multipart form")
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	type upload struct {
		name    string
		data    []byte
		debugID string
	}
	var ups []upload
	fields := make([]string, 0, len(r.MultipartForm.File))
	for f := range r.MultipartForm.File {
		fields = append(fields, f)
	}
	sort.Strings(fields)
	for _, field := range fields {
		for _, fh := range r.MultipartForm.File[field] {
			if len(ups) >= MaxUploadFiles {
				writeErr(w, http.StatusBadRequest, "too many files in one upload")
				return
			}
			name, err := artifacts.ArtifactName(field)
			if err != nil {
				writeErr(w, http.StatusBadRequest, "invalid file name "+field)
				return
			}
			if !strings.HasSuffix(name, ".map") {
				name += ".map"
			}
			if fh.Size > MaxSourcemapSize {
				writeErr(w, http.StatusRequestEntityTooLarge, name+" is larger than 20 MB")
				return
			}
			f, err := fh.Open()
			if err != nil {
				internal(w, err)
				return
			}
			data, err := io.ReadAll(io.LimitReader(f, MaxSourcemapSize+1))
			_ = f.Close()
			if err != nil {
				internal(w, err)
				return
			}
			m, err := sourcemap.Parse(data)
			if err != nil {
				writeErr(w, http.StatusBadRequest, name+": "+err.Error())
				return
			}
			ups = append(ups, upload{name: name, data: data, debugID: m.DebugID})
		}
	}
	if len(ups) == 0 {
		writeErr(w, http.StatusBadRequest, "no source map files in the upload")
		return
	}
	rel, err := s.Releases.EnsureRelease(r.Context(), projectID, version)
	if err != nil {
		internal(w, err)
		return
	}
	out := make([]gen.Artifact, 0, len(ups))
	for _, u := range ups {
		sha := artifacts.SHA256(u.data)
		key := artifacts.ObjectKey(projectID, sha)
		if err := s.ArtifactObjects.Put(r.Context(), key, u.data); err != nil {
			internal(w, err)
			return
		}
		a, err := s.Releases.PutArtifact(r.Context(), artifacts.Artifact{
			ReleaseID: rel.ID, ProjectID: projectID, Name: u.name, DebugID: u.debugID,
			ObjectKey: key, Size: int64(len(u.data)), SHA256: sha,
		})
		if err != nil {
			internal(w, err)
			return
		}
		out = append(out, toArtifact(a))
	}
	// Frames that missed a map a moment ago can resolve now.
	s.Symbolicator.Forget()
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) ListUploadTokens(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath) {
	if !canRead(r, projectID) {
		writeErr(w, http.StatusNotFound, "project not found")
		return
	}
	if !s.releasesOK(w) {
		return
	}
	list, err := s.Releases.ListTokens(r.Context(), projectID)
	if err != nil {
		internal(w, err)
		return
	}
	out := make([]gen.UploadToken, 0, len(list))
	for _, t := range list {
		out = append(out, toToken(t))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) CreateUploadToken(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath) {
	if !owns(w, r, projectID) || !s.releasesOK(w) {
		return
	}
	var body gen.UploadTokenCreate
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" || len(name) > 100 {
		writeErr(w, http.StatusBadRequest, "name must be 1 to 100 characters")
		return
	}
	var by int64
	if p := auth.FromContext(r.Context()); p != nil {
		by = p.User.ID
	}
	t, secret, err := s.Releases.CreateToken(r.Context(), projectID, name, by)
	if err != nil {
		internal(w, err)
		return
	}
	s.record(r, audit.Entry{ProjectID: projectID, Action: audit.UploadTokenCreate, Target: name})
	writeJSON(w, http.StatusCreated, gen.UploadTokenCreated{Token: toToken(t), Secret: secret})
}

func (s *Server) RevokeUploadToken(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath, tokenID int64) {
	if !owns(w, r, projectID) || !s.releasesOK(w) {
		return
	}
	t, err := s.Releases.RevokeToken(r.Context(), projectID, tokenID)
	if errors.Is(err, artifacts.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "token not found")
		return
	} else if err != nil {
		internal(w, err)
		return
	}
	s.record(r, audit.Entry{ProjectID: projectID, Action: audit.UploadTokenRevoke, Target: t.Name})
	writeJSON(w, http.StatusOK, toToken(t))
}

// stackFrames returns an event's frames, resolving them again when asked or
// when nothing mapped at ingest (maps are often uploaded after the deploy).
// Grouping is not changed after the fact.
func (s *Server) stackFrames(ctx context.Context, project int64, e ch.ErrorRecord, again bool) ([]symbolicate.Frame, int) {
	var frames []symbolicate.Frame
	_ = json.Unmarshal([]byte(e.Frames), &frames)
	mapped := int(e.MappedFrames)
	if s.Symbolicator != nil && (again || mapped == 0) && e.Stack != "" {
		ids := map[string]string{}
		_ = json.Unmarshal([]byte(e.DebugIDs), &ids)
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if res := s.Symbolicator.Resolve(ctx, project, e.Release, ids, e.Stack); res.Mapped >= mapped && len(res.Frames) > 0 {
			frames, mapped = res.Frames, res.Mapped
		}
	}
	return frames, mapped
}

func toFrames(in []symbolicate.Frame) *[]gen.StackFrame {
	if len(in) == 0 {
		return nil
	}
	out := make([]gen.StackFrame, len(in))
	for i, f := range in {
		out[i] = gen.StackFrame{
			Function: f.Function, File: f.File, Line: f.Line, Column: f.Column, InApp: f.InApp, Mapped: f.Mapped,
			ContextLine: strPtr(f.ContextLine), RawFunction: strPtr(f.RawFunction),
			RawUrl: f.RawURL, RawLine: f.RawLine, RawColumn: f.RawColumn,
		}
		if len(f.PreContext) > 0 {
			pre := f.PreContext
			out[i].PreContext = &pre
		}
		if len(f.PostContext) > 0 {
			post := f.PostContext
			out[i].PostContext = &post
		}
	}
	return &out
}
