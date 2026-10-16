// Package analytics is the product analytics module of query-api: events,
// funnels, trends, persons, retention and paths.
package analytics

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Mutiur03/pulse/internal/modules/analytics/adapters/httpapi"
	"github.com/Mutiur03/pulse/internal/modules/analytics/adapters/httpapi/oapi"
	"github.com/Mutiur03/pulse/internal/modules/analytics/ports"
	"github.com/Mutiur03/pulse/internal/modules/analytics/service"
)

// Readers the module needs; *ch.Client implements all of them.
type (
	Events = ports.Events
	Trends = ports.Trends
	People = ports.People
)

// Deps are the module's dependencies. Nil Trends or People disable those
// endpoints.
type Deps struct {
	Events Events
	Trends Trends
	People People
	Now    func() time.Time
}

// Module is the analytics module.
type Module struct{ handlers *httpapi.Handlers }

// New wires the module.
func New(d Deps) *Module {
	return &Module{handlers: httpapi.New(service.New(d.Events, d.Trends, d.People), d.Now)}
}

// Register mounts the module's routes on r.
func (m *Module) Register(r chi.Router) {
	oapi.HandlerWithOptions(m.handlers.Handler(), oapi.ChiServerOptions{BaseRouter: r.With(maxBody)})
}

// CheckWidget validates a saved dashboard widget query by kind.
func CheckWidget(kind string, query json.RawMessage) error { return httpapi.CheckWidget(kind, query) }

// maxBody caps request bodies: trend and funnel queries may be larger than
// retention and paths queries.
func maxBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int64(httpapi.MaxSmallBody)
		if r.URL.Path == "/v1/insights/trends" || r.URL.Path == "/v1/funnels/query" {
			n = httpapi.MaxQueryBody
		}
		r.Body = http.MaxBytesReader(w, r.Body, n)
		next.ServeHTTP(w, r)
	})
}
