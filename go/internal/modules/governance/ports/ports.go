// Package ports declares what the governance module reads and writes.
package ports

import (
	"context"

	"github.com/Mutiur03/pulse/internal/audit"
	"github.com/Mutiur03/pulse/internal/governance"
)

// Store keeps data settings and deletion requests.
type Store interface {
	Settings(ctx context.Context, project int64) (governance.Settings, error)
	SaveSettings(ctx context.Context, s governance.Settings) (governance.Settings, error)
	CreateRequest(ctx context.Context, r governance.Request) (governance.Request, error)
	ListRequests(ctx context.Context, project int64, limit int) ([]governance.Request, error)
	GetRequest(ctx context.Context, project, id int64) (governance.Request, error)
}

// Persons finds every id linked to a person.
type Persons interface {
	ResolvePerson(ctx context.Context, project uint64, distinctID string) (string, error)
	LinkedIDs(ctx context.Context, project uint64, ids []string) ([]string, error)
}

// AuditLog lists an organization's audit entries.
type AuditLog interface {
	List(ctx context.Context, org int64, limit int, before int64) ([]audit.Entry, error)
}
