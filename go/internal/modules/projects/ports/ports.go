// Package ports declares what the projects module reads and writes.
package ports

import (
	"context"

	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/keys"
)

// Projects stores projects.
type Projects interface {
	List(ctx context.Context, userID int64) ([]auth.Project, error)
	Create(ctx context.Context, userID, orgID int64, name string) (auth.Project, keys.APIKey, error)
	Rename(ctx context.Context, projectID int64, name string) (string, error)
}

// Keys manages ingest keys; Purge drops revoked keys from the gateway cache.
type Keys interface {
	List(ctx context.Context, projectID int64) ([]keys.APIKey, error)
	Rotate(ctx context.Context, projectID int64) (keys.APIKey, []string, error)
	Create(ctx context.Context, projectID int64) (keys.APIKey, error)
	Revoke(ctx context.Context, projectID, keyID int64) (keys.APIKey, error)
	Purge(ctx context.Context, publicKey string) error
}

// Origins keeps the browser origins allowed to send data.
type Origins interface {
	AllowedOrigins(ctx context.Context, project int64) ([]string, error)
	SetAllowedOrigins(ctx context.Context, project int64, origins []string) ([]string, error)
}
