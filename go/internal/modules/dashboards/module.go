// Package dashboards is the saved dashboards module of query-api.
package dashboards

import (
	"encoding/json"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Mutiur03/pulse/internal/apikit"
	"github.com/Mutiur03/pulse/internal/modules/dashboards/adapters/httpapi"
	"github.com/Mutiur03/pulse/internal/modules/dashboards/adapters/httpapi/oapi"
	"github.com/Mutiur03/pulse/internal/modules/dashboards/adapters/postgres"
	"github.com/Mutiur03/pulse/internal/modules/dashboards/domain"
	"github.com/Mutiur03/pulse/internal/modules/dashboards/ports"
	"github.com/Mutiur03/pulse/internal/modules/dashboards/service"
)

// Public types for callers that build or fake the module's storage.
type (
	Dashboard  = domain.Dashboard
	Widget     = domain.Widget
	Repository = ports.Repository
)

// ErrNotFound means the dashboard does not exist or the user cannot see it.
var ErrNotFound = domain.ErrNotFound

// Deps are the module's dependencies. Repository, when set, replaces the
// Postgres store built from DB; with neither, dashboards are disabled.
type Deps struct {
	DB         *pgxpool.Pool
	Repository Repository
	// CheckWidget validates a saved widget query by kind.
	CheckWidget func(kind string, query json.RawMessage) error
}

// Module is the dashboards module.
type Module struct{ handlers *httpapi.Handlers }

// New wires the module.
func New(d Deps) *Module {
	repo := d.Repository
	if repo == nil && d.DB != nil {
		repo = &postgres.Store{DB: d.DB}
	}
	var svc *service.Service
	if repo != nil {
		svc = service.New(repo, d.CheckWidget)
	}
	return &Module{handlers: httpapi.New(svc)}
}

// Register mounts the module's routes on r.
func (m *Module) Register(r chi.Router) {
	oapi.HandlerWithOptions(m.handlers.Handler(), oapi.ChiServerOptions{BaseRouter: r.With(apikit.LimitBody(httpapi.MaxBody))})
}
