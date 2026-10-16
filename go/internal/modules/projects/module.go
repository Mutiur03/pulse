// Package projects is the projects module of query-api: projects, ingest
// keys and allowed browser origins. Storage lives in internal/projects and
// internal/keys, shared with the ingest gateway.
package projects

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Mutiur03/pulse/internal/apikit"
	"github.com/Mutiur03/pulse/internal/audit"
	"github.com/Mutiur03/pulse/internal/clientip"
	"github.com/Mutiur03/pulse/internal/modules/projects/adapters/httpapi"
	"github.com/Mutiur03/pulse/internal/modules/projects/adapters/httpapi/oapi"
	"github.com/Mutiur03/pulse/internal/modules/projects/ports"
	"github.com/Mutiur03/pulse/internal/modules/projects/service"
)

// Dependencies as interfaces, for callers and tests.
type (
	Projects = ports.Projects
	Keys     = ports.Keys
	Origins  = ports.Origins
)

// Deps are the module's dependencies. Nil Origins lists none and refuses
// changes.
type Deps struct {
	Projects Projects
	Keys     Keys
	Origins  Origins
	Audit    audit.Recorder
	ClientIP *clientip.Resolver
}

// Module is the projects module.
type Module struct{ handlers *httpapi.Handlers }

// New wires the module.
func New(d Deps) *Module {
	svc := &service.Service{Projects: d.Projects, Keys: d.Keys, Origins: d.Origins}
	return &Module{handlers: httpapi.New(svc, apikit.Auditor{Recorder: d.Audit, ClientIP: d.ClientIP})}
}

// Register mounts the module's routes on r.
func (m *Module) Register(r chi.Router) {
	oapi.HandlerWithOptions(m.handlers.Handler(), oapi.ChiServerOptions{BaseRouter: r.With(maxBody)})
}

// maxBody caps request bodies; project create and rename take only a name.
func maxBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int64(httpapi.MaxBody)
		if rest, ok := strings.CutPrefix(r.URL.Path, "/v1/projects"); ok && !strings.Contains(strings.TrimPrefix(rest, "/"), "/") {
			n = httpapi.MaxNameBody
		}
		r.Body = http.MaxBytesReader(w, r.Body, n)
		next.ServeHTTP(w, r)
	})
}
