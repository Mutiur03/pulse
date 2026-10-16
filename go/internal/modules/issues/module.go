// Package issues is the error issues module of query-api. Its public API is
// also what the stream processor and alerting worker use to record issues
// and read issue events.
package issues

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Mutiur03/pulse/internal/modules/issues/adapters/httpapi"
	"github.com/Mutiur03/pulse/internal/modules/issues/adapters/httpapi/oapi"
	"github.com/Mutiur03/pulse/internal/modules/issues/adapters/postgres"
	"github.com/Mutiur03/pulse/internal/modules/issues/domain"
	"github.com/Mutiur03/pulse/internal/modules/issues/ports"
	"github.com/Mutiur03/pulse/internal/modules/issues/service"
	"github.com/Mutiur03/pulse/internal/symbolicate"
)

// Public types.
type (
	Issue       = domain.Issue
	Status      = domain.Status
	Kind        = domain.Kind
	Occurrence  = domain.Occurrence
	Event       = domain.Event
	ListFilter  = domain.ListFilter
	Repository  = ports.Repository
	Occurrences = ports.Occurrences
	// Store is the Postgres issue store.
	Store = postgres.Store
)

// Statuses and occurrence kinds.
const (
	Unresolved     = domain.Unresolved
	Resolved       = domain.Resolved
	Ignored        = domain.Ignored
	KindNone       = domain.KindNone
	KindNew        = domain.KindNew
	KindRegression = domain.KindRegression
)

// ErrNotFound means the issue does not exist.
var ErrNotFound = domain.ErrNotFound

// Transition decides the issue status after a new occurrence.
func Transition(exists bool, cur Status, resolvedAt *time.Time, eventAt time.Time) (Status, Kind) {
	return domain.Transition(exists, cur, resolvedAt, eventAt)
}

// Connect opens the Postgres issue store.
func Connect(ctx context.Context, url string) (*Store, error) { return postgres.Connect(ctx, url) }

// NewStore returns an issue store on an existing pool.
func NewStore(db *pgxpool.Pool) *Store { return postgres.New(db) }

// Deps are the module's dependencies.
type Deps struct {
	Repository  Repository
	Occurrences Occurrences
	// Symbolicator re-resolves stacks on read; nil skips it.
	Symbolicator *symbolicate.Symbolicator
	Now          func() time.Time
}

// Module is the issues module.
type Module struct{ handlers *httpapi.Handlers }

// New wires the module.
func New(d Deps) *Module {
	return &Module{handlers: httpapi.New(service.New(d.Repository, d.Occurrences), d.Symbolicator, d.Now)}
}

// Register mounts the module's routes on r.
func (m *Module) Register(r chi.Router) {
	oapi.HandlerWithOptions(m.handlers.Handler(), oapi.ChiServerOptions{BaseRouter: r.With(maxBody)})
}

func maxBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, httpapi.MaxBody)
		next.ServeHTTP(w, r)
	})
}
