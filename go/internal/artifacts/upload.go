package artifacts

import (
	"context"
	"errors"
	"io"
	"mime/multipart"
	"sort"
	"strconv"
	"strings"

	"github.com/Mutiur03/pulse/internal/sourcemap"
)

// Upload is one validated source map from an upload request.
type Upload struct {
	Name    string
	Data    []byte
	DebugID string
}

// UploadError is a problem with the uploaded files that the client can fix.
// TooLarge distinguishes size limits from other bad input.
type UploadError struct {
	Msg      string
	TooLarge bool
}

func (e *UploadError) Error() string { return e.Msg }

func badUpload(msg string) error { return &UploadError{Msg: msg} }

// ReadUploads validates every file of a multipart form as a source map, in
// field name order. Names are normalised with ArtifactName and get a .map
// suffix. At most maxFiles files of maxSize bytes each are accepted.
func ReadUploads(form *multipart.Form, maxFiles int, maxSize int64) ([]Upload, error) {
	fields := make([]string, 0, len(form.File))
	for f := range form.File {
		fields = append(fields, f)
	}
	sort.Strings(fields)
	var ups []Upload
	for _, field := range fields {
		for _, fh := range form.File[field] {
			if len(ups) >= maxFiles {
				return nil, badUpload("too many files in one upload")
			}
			u, err := readUpload(field, fh, maxSize)
			if err != nil {
				return nil, err
			}
			ups = append(ups, u)
		}
	}
	if len(ups) == 0 {
		return nil, badUpload("no source map files in the upload")
	}
	return ups, nil
}

func readUpload(field string, fh *multipart.FileHeader, maxSize int64) (Upload, error) {
	name, err := ArtifactName(field)
	if err != nil {
		return Upload{}, badUpload("invalid file name " + field)
	}
	if !strings.HasSuffix(name, ".map") {
		name += ".map"
	}
	if fh.Size > maxSize {
		return Upload{}, &UploadError{Msg: name + " is larger than " + sizeMB(maxSize), TooLarge: true}
	}
	f, err := fh.Open()
	if err != nil {
		return Upload{}, err
	}
	data, err := io.ReadAll(io.LimitReader(f, maxSize+1))
	_ = f.Close()
	if err != nil {
		return Upload{}, err
	}
	m, err := sourcemap.Parse(data)
	if err != nil {
		return Upload{}, badUpload(name + ": " + err.Error())
	}
	return Upload{Name: name, Data: data, DebugID: m.DebugID}, nil
}

func sizeMB(n int64) string { return strconv.FormatInt(n>>20, 10) + " MB" }

// ReleaseWriter is the part of Store that Publish needs.
type ReleaseWriter interface {
	EnsureRelease(ctx context.Context, project int64, version string) (Release, error)
	PutArtifact(ctx context.Context, a Artifact) (Artifact, error)
}

// ObjectPutter stores artifact bytes.
type ObjectPutter interface {
	Put(ctx context.Context, key string, data []byte) error
}

// Publish stores the uploads' bytes under content-addressed keys and records
// them as artifacts of the release, creating the release if needed.
func Publish(ctx context.Context, rs ReleaseWriter, objects ObjectPutter, project int64, version string, ups []Upload) ([]Artifact, error) {
	if len(ups) == 0 {
		return nil, errors.New("nothing to publish")
	}
	rel, err := rs.EnsureRelease(ctx, project, version)
	if err != nil {
		return nil, err
	}
	out := make([]Artifact, 0, len(ups))
	for _, u := range ups {
		sha := SHA256(u.Data)
		key := ObjectKey(project, sha)
		if err := objects.Put(ctx, key, u.Data); err != nil {
			return nil, err
		}
		a, err := rs.PutArtifact(ctx, Artifact{
			ReleaseID: rel.ID, ProjectID: project, Name: u.Name, DebugID: u.DebugID,
			ObjectKey: key, Size: int64(len(u.Data)), SHA256: sha,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}
