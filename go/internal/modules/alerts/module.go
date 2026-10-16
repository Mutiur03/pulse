// Package alerts is the alert rules and channels module of query-api. Rule
// evaluation lives in internal/alerts, shared with the alerting worker.
package alerts

import (
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Mutiur03/pulse/internal/apikit"
	"github.com/Mutiur03/pulse/internal/audit"
	"github.com/Mutiur03/pulse/internal/clientip"
	"github.com/Mutiur03/pulse/internal/modules/alerts/adapters/httpapi"
	"github.com/Mutiur03/pulse/internal/modules/alerts/adapters/httpapi/oapi"
	"github.com/Mutiur03/pulse/internal/modules/alerts/ports"
	"github.com/Mutiur03/pulse/internal/modules/alerts/service"
)

// Dependencies as interfaces, for callers and tests.
type (
	Store         = ports.Store
	Metrics       = ports.Metrics
	Sender        = ports.Sender
	OutboundCheck = ports.OutboundCheck
)

// Deps are the module's dependencies. A nil Store disables alerting, nil
// Metrics empty previews and a nil Sender channel tests.
type Deps struct {
	Store         Store
	Metrics       Metrics
	Sender        Sender
	OutboundCheck OutboundCheck
	AppURL        string
	Audit         audit.Recorder
	ClientIP      *clientip.Resolver
	Now           func() time.Time
}

// Module is the alerts module.
type Module struct{ handlers *httpapi.Handlers }

// New wires the module.
func New(d Deps) *Module {
	svc := &service.Service{Store: d.Store, Metrics: d.Metrics, Sender: d.Sender, Check: d.OutboundCheck, AppURL: d.AppURL}
	return &Module{handlers: httpapi.New(svc, apikit.Auditor{Recorder: d.Audit, ClientIP: d.ClientIP}, d.Now)}
}

// Register mounts the module's routes on r.
func (m *Module) Register(r chi.Router) {
	oapi.HandlerWithOptions(m.handlers.Handler(), oapi.ChiServerOptions{BaseRouter: r.With(apikit.LimitBody(httpapi.MaxBody))})
}
