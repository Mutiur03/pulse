// Package replay is the session replay module of query-api: listing
// recordings, their errors and traces, and playback events.
package replay

import (
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Mutiur03/pulse/internal/modules/replay/adapters/httpapi"
	"github.com/Mutiur03/pulse/internal/modules/replay/adapters/httpapi/oapi"
	"github.com/Mutiur03/pulse/internal/modules/replay/domain"
	"github.com/Mutiur03/pulse/internal/modules/replay/ports"
	"github.com/Mutiur03/pulse/internal/modules/replay/service"
)

// DefaultRetentionDays matches the TTL of the replay_chunks table.
const DefaultRetentionDays = domain.DefaultRetentionDays

// Reader and Objects are the storage the module needs.
type (
	Reader  = ports.Reader
	Objects = ports.Objects
)

// Deps are the module's dependencies. A nil Reader disables replay; a nil
// Objects disables playback.
type Deps struct {
	Reader        Reader
	Objects       Objects
	RetentionDays int
	Now           func() time.Time
}

// Module is the replay module.
type Module struct {
	handlers *httpapi.Handlers
}

// New wires the module.
func New(d Deps) *Module {
	svc := service.New(d.Reader, d.Objects, d.RetentionDays, d.Now)
	return &Module{handlers: httpapi.New(svc, d.Now)}
}

// Register mounts the module's routes on r.
func (m *Module) Register(r chi.Router) {
	oapi.HandlerFromMux(m.handlers.Handler(), r)
}
