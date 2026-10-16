// Package telemetry is the trace, log and metric query module of
// query-api.
package telemetry

import (
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Mutiur03/pulse/internal/modules/telemetry/adapters/httpapi"
	"github.com/Mutiur03/pulse/internal/modules/telemetry/adapters/httpapi/oapi"
	"github.com/Mutiur03/pulse/internal/modules/telemetry/ports"
	"github.com/Mutiur03/pulse/internal/modules/telemetry/service"
)

// Readers the module needs; *ch.Client implements all of them.
type (
	Traces  = ports.Traces
	Stats   = ports.Stats
	Logs    = ports.Logs
	Metrics = ports.Metrics
)

// Deps are the module's dependencies. Nil Stats, Logs or Metrics disable
// those endpoints.
type Deps struct {
	Traces  Traces
	Stats   Stats
	Logs    Logs
	Metrics Metrics
	Now     func() time.Time
}

// Module is the telemetry module.
type Module struct{ handlers *httpapi.Handlers }

// New wires the module.
func New(d Deps) *Module {
	return &Module{handlers: httpapi.New(service.New(d.Traces, d.Stats, d.Logs, d.Metrics), d.Now)}
}

// Register mounts the module's routes on r.
func (m *Module) Register(r chi.Router) {
	oapi.HandlerFromMux(m.handlers.Handler(), r)
}
