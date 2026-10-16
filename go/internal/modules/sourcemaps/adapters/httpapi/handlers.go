// Package httpapi serves the release and source map endpoints. JSON
// operations use the generated strict interface; the multipart upload is a
// plain handler.
package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/apikit"
	"github.com/Mutiur03/pulse/internal/audit"
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/httpx"
	"github.com/Mutiur03/pulse/internal/modules/sourcemaps/adapters/httpapi/oapi"
	"github.com/Mutiur03/pulse/internal/modules/sourcemaps/domain"
	"github.com/Mutiur03/pulse/internal/modules/sourcemaps/service"
)

// Handlers implements oapi.StrictServerInterface. A nil service means
// releases are not enabled.
type Handlers struct {
	svc   *service.Service
	audit apikit.Auditor
}

var _ oapi.StrictServerInterface = (*Handlers)(nil)

// New returns the handlers.
func New(svc *service.Service, a apikit.Auditor) *Handlers { return &Handlers{svc: svc, audit: a} }

// server swaps in the plain handlers for operations the strict interface
// does not fit.
type server struct {
	oapi.ServerInterface
	h *Handlers
}

// Handler returns the routes as an oapi.ServerInterface for mounting.
func (h *Handlers) Handler() oapi.ServerInterface {
	strict := oapi.NewStrictHandlerWithOptions(h, []oapi.StrictMiddlewareFunc{apikit.WithRequest},
		oapi.StrictHTTPServerOptions(apikit.Strict("invalid JSON")))
	return server{ServerInterface: strict, h: h}
}

// CreateUploadToken caps the JSON body before the strict handler reads it.
func (s server) CreateUploadToken(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<14)
	s.ServerInterface.CreateUploadToken(w, r, projectID)
}

// UploadSourcemaps stores each multipart file as a source map of the
// release. It authenticates with an upload token only; see auth.Middleware.
func (s server) UploadSourcemaps(w http.ResponseWriter, r *http.Request, projectID gen.ProjectPath, version gen.ReleasePath) {
	svc := s.h.svc
	if svc == nil {
		httpx.WriteError(w, http.StatusNotFound, "releases are not enabled")
		return
	}
	if !svc.CanStore() {
		httpx.WriteError(w, http.StatusServiceUnavailable, "object storage is not configured")
		return
	}
	secret, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if err := svc.Authorize(r.Context(), projectID, secret); err != nil {
		apikit.WriteError(w, mapErr(err))
		return
	}
	if !domain.ValidVersion(version) {
		httpx.WriteError(w, http.StatusBadRequest, "release may use letters, digits and . _ - + @, up to 200 characters")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, service.MaxUploadBytes)
	if err := r.ParseMultipartForm(8 << 20); err != nil { //nolint:gosec // body capped by MaxBytesReader above
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, "upload is larger than 50 MB")
			return
		}
		httpx.WriteError(w, http.StatusBadRequest, "expected a multipart form")
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	arts, err := svc.Upload(r.Context(), projectID, version, r.MultipartForm)
	if err != nil {
		apikit.WriteError(w, mapErr(err))
		return
	}
	out := make([]gen.Artifact, 0, len(arts))
	for _, a := range arts {
		out = append(out, toArtifact(a))
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func mapErr(err error) error {
	var bad *domain.UploadError
	switch {
	case errors.As(err, &bad):
		if bad.TooLarge {
			return apikit.Err(http.StatusRequestEntityTooLarge, bad.Msg)
		}
		return apikit.Err(http.StatusBadRequest, bad.Msg)
	case errors.Is(err, service.ErrBadToken):
		return apikit.Err(http.StatusUnauthorized, err.Error())
	case errors.Is(err, service.ErrNoStorage):
		return apikit.Err(http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, service.ErrBadName):
		return apikit.Err(http.StatusBadRequest, err.Error())
	}
	return err
}

// ready checks the user can read the project and releases are enabled.
func (h *Handlers) ready(ctx context.Context, project int64) error {
	if !apikit.CanRead(ctx, project) {
		return apikit.Err(http.StatusNotFound, "project not found")
	}
	return h.enabled()
}

func (h *Handlers) enabled() error {
	if h.svc == nil {
		return apikit.Err(http.StatusNotFound, "releases are not enabled")
	}
	return nil
}

func toRelease(r domain.Release) gen.Release {
	return gen.Release{Id: r.ID, Version: r.Version, CreatedAt: r.CreatedAt, ArtifactCount: int(r.Artifacts)}
}

func toArtifact(a domain.Artifact) gen.Artifact {
	return gen.Artifact{Id: a.ID, Name: a.Name, DebugId: apikit.StrPtr(a.DebugID), Size: a.Size, Sha256: a.SHA256, CreatedAt: a.CreatedAt}
}

func toToken(t domain.Token) gen.UploadToken {
	return gen.UploadToken{Id: t.ID, Name: t.Name, Hint: t.Hint, CreatedAt: t.CreatedAt, LastUsedAt: t.LastUsedAt, RevokedAt: t.RevokedAt}
}

// ListReleases implements GET /v1/projects/{project_id}/releases.
func (h *Handlers) ListReleases(ctx context.Context, req oapi.ListReleasesRequestObject) (oapi.ListReleasesResponseObject, error) {
	if err := h.ready(ctx, req.ProjectId); err != nil {
		return nil, err
	}
	limit := 50
	if l := req.Params.Limit; l != nil && *l >= 1 && *l <= 200 {
		limit = *l
	}
	list, err := h.svc.ListReleases(ctx, req.ProjectId, limit)
	if err != nil {
		return nil, err
	}
	out := make([]gen.Release, 0, len(list))
	for _, rel := range list {
		out = append(out, toRelease(rel))
	}
	return oapi.ListReleases200JSONResponse(out), nil
}

// GetRelease implements GET /v1/projects/{project_id}/releases/{release}.
func (h *Handlers) GetRelease(ctx context.Context, req oapi.GetReleaseRequestObject) (oapi.GetReleaseResponseObject, error) {
	if err := h.ready(ctx, req.ProjectId); err != nil {
		return nil, err
	}
	rel, arts, err := h.svc.GetRelease(ctx, req.ProjectId, req.Release)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, apikit.Err(http.StatusNotFound, "release not found")
	} else if err != nil {
		return nil, err
	}
	out := gen.ReleaseDetail{Release: toRelease(rel), Artifacts: make([]gen.Artifact, 0, len(arts))}
	for _, a := range arts {
		out.Artifacts = append(out.Artifacts, toArtifact(a))
	}
	return oapi.GetRelease200JSONResponse(out), nil
}

// UploadSourcemaps is served by server.UploadSourcemaps.
func (h *Handlers) UploadSourcemaps(context.Context, oapi.UploadSourcemapsRequestObject) (oapi.UploadSourcemapsResponseObject, error) {
	return nil, errors.New("served by the multipart handler")
}

// ListUploadTokens implements GET /v1/projects/{project_id}/upload-tokens.
func (h *Handlers) ListUploadTokens(ctx context.Context, req oapi.ListUploadTokensRequestObject) (oapi.ListUploadTokensResponseObject, error) {
	if err := h.ready(ctx, req.ProjectId); err != nil {
		return nil, err
	}
	list, err := h.svc.ListTokens(ctx, req.ProjectId)
	if err != nil {
		return nil, err
	}
	out := make([]gen.UploadToken, 0, len(list))
	for _, t := range list {
		out = append(out, toToken(t))
	}
	return oapi.ListUploadTokens200JSONResponse(out), nil
}

// CreateUploadToken implements POST /v1/projects/{project_id}/upload-tokens.
func (h *Handlers) CreateUploadToken(ctx context.Context, req oapi.CreateUploadTokenRequestObject) (oapi.CreateUploadTokenResponseObject, error) {
	if err := apikit.Owner(ctx, req.ProjectId); err != nil {
		return nil, err
	}
	if err := h.enabled(); err != nil {
		return nil, err
	}
	var by int64
	if p := auth.FromContext(ctx); p != nil {
		by = p.User.ID
	}
	t, secret, err := h.svc.CreateToken(ctx, req.ProjectId, req.Body.Name, by)
	if err != nil {
		return nil, mapErr(err)
	}
	h.audit.Record(ctx, audit.Entry{ProjectID: req.ProjectId, Action: audit.UploadTokenCreate, Target: t.Name})
	return oapi.CreateUploadToken201JSONResponse(gen.UploadTokenCreated{Token: toToken(t), Secret: secret}), nil
}

// RevokeUploadToken implements POST /v1/projects/{project_id}/upload-tokens/{token_id}/revoke.
func (h *Handlers) RevokeUploadToken(ctx context.Context, req oapi.RevokeUploadTokenRequestObject) (oapi.RevokeUploadTokenResponseObject, error) {
	if err := apikit.Owner(ctx, req.ProjectId); err != nil {
		return nil, err
	}
	if err := h.enabled(); err != nil {
		return nil, err
	}
	t, err := h.svc.RevokeToken(ctx, req.ProjectId, req.TokenId)
	if errors.Is(err, domain.ErrNotFound) {
		return nil, apikit.Err(http.StatusNotFound, "token not found")
	} else if err != nil {
		return nil, err
	}
	h.audit.Record(ctx, audit.Entry{ProjectID: req.ProjectId, Action: audit.UploadTokenRevoke, Target: t.Name})
	return oapi.RevokeUploadToken200JSONResponse(toToken(t)), nil
}
