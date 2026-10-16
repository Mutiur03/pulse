// Package system is the health module of query-api.
package system

import (
	"github.com/go-chi/chi/v5"

	"github.com/Mutiur03/pulse/internal/modules/system/adapters/httpapi"
	"github.com/Mutiur03/pulse/internal/modules/system/adapters/httpapi/oapi"
)

// Module is the system module.
type Module struct{ handlers *httpapi.Handlers }

// New wires the module for the named service.
func New(service string) *Module { return &Module{handlers: httpapi.New(service)} }

// Register mounts the module's routes on r.
func (m *Module) Register(r chi.Router) { oapi.HandlerFromMux(m.handlers.Handler(), r) }
