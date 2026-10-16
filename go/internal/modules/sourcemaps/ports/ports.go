// Package ports declares what the sourcemaps module needs.
package ports

import (
	"context"

	"github.com/Mutiur03/pulse/internal/modules/sourcemaps/domain"
)

// ReleaseWriter is the part of Store that Publish needs.
type ReleaseWriter interface {
	EnsureRelease(ctx context.Context, project int64, version string) (domain.Release, error)
	PutArtifact(ctx context.Context, a domain.Artifact) (domain.Artifact, error)
}

// ObjectPutter stores artifact bytes.
type ObjectPutter interface {
	Put(ctx context.Context, key string, data []byte) error
}

// Repository keeps releases, artifacts and upload tokens.
type Repository interface {
	ReleaseWriter
	ListReleases(ctx context.Context, project int64, limit int) ([]domain.Release, error)
	GetRelease(ctx context.Context, project int64, version string) (domain.Release, []domain.Artifact, error)
	CreateToken(ctx context.Context, project int64, name string, by int64) (domain.Token, string, error)
	ListTokens(ctx context.Context, project int64) ([]domain.Token, error)
	RevokeToken(ctx context.Context, project, id int64) (domain.Token, error)
	Authenticate(ctx context.Context, secret string) (int64, error)
}
