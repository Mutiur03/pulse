package service

import (
	"context"
	"errors"
	"mime/multipart"
	"strings"

	"github.com/Mutiur03/pulse/internal/modules/sourcemaps/domain"
	"github.com/Mutiur03/pulse/internal/modules/sourcemaps/ports"
)

// Upload limits for one request and for each map in it.
const (
	MaxUploadBytes   = 50 << 20
	MaxSourcemapSize = 20 << 20
	MaxUploadFiles   = 200
)

// ErrNoStorage means uploads cannot be stored.
var ErrNoStorage = errors.New("object storage is not configured")

// ErrBadToken means the upload token is unknown or for another project.
var ErrBadToken = errors.New("invalid upload token")

// ErrBadName is an upload token name outside 1 to 100 characters.
var ErrBadName = errors.New("name must be 1 to 100 characters")

// Service manages releases, source map uploads and upload tokens.
type Service struct {
	repo    ports.Repository
	objects ports.ObjectPutter
	// uploaded runs after a successful upload, so frames that missed a map a
	// moment ago can resolve now.
	uploaded func()
}

// New returns a Service. objects may be nil (uploads are refused) and
// uploaded may be nil.
func New(repo ports.Repository, objects ports.ObjectPutter, uploaded func()) *Service {
	return &Service{repo: repo, objects: objects, uploaded: uploaded}
}

// ListReleases returns the project's newest releases.
func (s *Service) ListReleases(ctx context.Context, project int64, limit int) ([]domain.Release, error) {
	return s.repo.ListReleases(ctx, project, limit)
}

// GetRelease returns a release with its artifacts.
func (s *Service) GetRelease(ctx context.Context, project int64, version string) (domain.Release, []domain.Artifact, error) {
	return s.repo.GetRelease(ctx, project, version)
}

// CanStore reports whether uploads can be stored.
func (s *Service) CanStore() bool { return s.objects != nil }

// Authorize checks the upload token secret belongs to the project.
func (s *Service) Authorize(ctx context.Context, project int64, secret string) error {
	owner, err := s.repo.Authenticate(ctx, strings.TrimSpace(secret))
	if errors.Is(err, domain.ErrNotFound) || (err == nil && owner != project) {
		return ErrBadToken
	} else if err != nil {
		return err
	}
	return nil
}

// Upload stores every file of the form as a source map of the release.
func (s *Service) Upload(ctx context.Context, project int64, version string, form *multipart.Form) ([]domain.Artifact, error) {
	if s.objects == nil {
		return nil, ErrNoStorage
	}
	ups, err := ReadUploads(form, MaxUploadFiles, MaxSourcemapSize)
	if err != nil {
		return nil, err
	}
	arts, err := Publish(ctx, s.repo, s.objects, project, version, ups)
	if err != nil {
		return nil, err
	}
	if s.uploaded != nil {
		s.uploaded()
	}
	return arts, nil
}

// ListTokens returns the project's upload tokens.
func (s *Service) ListTokens(ctx context.Context, project int64) ([]domain.Token, error) {
	return s.repo.ListTokens(ctx, project)
}

// CreateToken adds a named upload token and returns it with its secret.
func (s *Service) CreateToken(ctx context.Context, project int64, name string, by int64) (domain.Token, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return domain.Token{}, "", ErrBadName
	}
	return s.repo.CreateToken(ctx, project, name, by)
}

// RevokeToken revokes an upload token.
func (s *Service) RevokeToken(ctx context.Context, project, id int64) (domain.Token, error) {
	return s.repo.RevokeToken(ctx, project, id)
}
