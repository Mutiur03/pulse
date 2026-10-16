// Package sourcemaps is the releases and source map module of query-api.
// The pipeline services use its Store to resolve and expire source maps.
package sourcemaps

import (
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Mutiur03/pulse/internal/apikit"
	"github.com/Mutiur03/pulse/internal/audit"
	"github.com/Mutiur03/pulse/internal/clientip"
	"github.com/Mutiur03/pulse/internal/modules/sourcemaps/adapters/httpapi"
	"github.com/Mutiur03/pulse/internal/modules/sourcemaps/adapters/httpapi/oapi"
	"github.com/Mutiur03/pulse/internal/modules/sourcemaps/adapters/postgres"
	"github.com/Mutiur03/pulse/internal/modules/sourcemaps/domain"
	"github.com/Mutiur03/pulse/internal/modules/sourcemaps/ports"
	"github.com/Mutiur03/pulse/internal/modules/sourcemaps/service"
)

// Public types.
type (
	// Store keeps releases, artifacts and upload tokens in Postgres. It
	// also serves symbolicate.Artifacts and the governance expiry.
	Store        = postgres.Store
	Release      = domain.Release
	Artifact     = domain.Artifact
	Token        = domain.Token
	Repository   = ports.Repository
	ObjectPutter = ports.ObjectPutter
)

// ErrNotFound is returned for releases, artifacts or tokens outside the project.
var ErrNotFound = domain.ErrNotFound

// NewStore returns the Postgres store.
func NewStore(db *pgxpool.Pool) *Store { return &postgres.Store{DB: db} }

// Deps are the module's dependencies. With no Repository, releases are
// disabled; with no Objects, uploads are refused.
type Deps struct {
	Repository Repository
	Objects    ObjectPutter
	// Uploaded runs after each successful upload (e.g. to clear a
	// symbolication cache).
	Uploaded func()
	Audit    audit.Recorder
	ClientIP *clientip.Resolver
}

// Module is the sourcemaps module.
type Module struct{ handlers *httpapi.Handlers }

// New wires the module.
func New(d Deps) *Module {
	var svc *service.Service
	if d.Repository != nil {
		svc = service.New(d.Repository, d.Objects, d.Uploaded)
	}
	return &Module{handlers: httpapi.New(svc, apikit.Auditor{Recorder: d.Audit, ClientIP: d.ClientIP})}
}

// Register mounts the module's routes on r.
func (m *Module) Register(r chi.Router) {
	oapi.HandlerFromMux(m.handlers.Handler(), r)
}

// ObjectKey is the storage key of a map's contents.
func ObjectKey(project int64, sha string) string { return domain.ObjectKey(project, sha) }

// SHA256 is the hex digest used to name stored maps.
func SHA256(data []byte) string { return domain.SHA256(data) }
