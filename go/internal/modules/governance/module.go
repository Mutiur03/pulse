// Package governance is the data governance module of query-api: PII
// scrubbing settings, retention, person deletion requests and the
// organization audit log. Scrubbing and retention themselves live in
// internal/governance, shared with the ingest gateway and governance worker.
package governance

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Mutiur03/pulse/internal/apikit"
	"github.com/Mutiur03/pulse/internal/audit"
	"github.com/Mutiur03/pulse/internal/clientip"
	"github.com/Mutiur03/pulse/internal/modules/governance/adapters/httpapi"
	"github.com/Mutiur03/pulse/internal/modules/governance/adapters/httpapi/oapi"
	"github.com/Mutiur03/pulse/internal/modules/governance/ports"
	"github.com/Mutiur03/pulse/internal/modules/governance/service"
)

// Dependencies as interfaces, for callers and tests.
type (
	Store    = ports.Store
	Persons  = ports.Persons
	AuditLog = ports.AuditLog
)

// Deps are the module's dependencies. A nil Store disables settings and
// deletions; nil Persons deletes only the given id; a nil AuditLog lists
// nothing.
type Deps struct {
	Store    Store
	Persons  Persons
	AuditLog AuditLog
	Audit    audit.Recorder
	ClientIP *clientip.Resolver
}

// Module is the governance module.
type Module struct{ handlers *httpapi.Handlers }

// New wires the module.
func New(d Deps) *Module {
	svc := &service.Service{Store: d.Store, Persons: d.Persons, AuditLog: d.AuditLog}
	return &Module{handlers: httpapi.New(svc, apikit.Auditor{Recorder: d.Audit, ClientIP: d.ClientIP})}
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
