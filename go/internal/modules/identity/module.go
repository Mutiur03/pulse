// Package identity is the accounts and organizations module of query-api:
// login, signup, sessions, password reset, email verification, members and
// invites. Users, sessions and organizations are stored by internal/auth
// and internal/orgs, which the auth middleware also uses.
package identity

import (
	"github.com/go-chi/chi/v5"

	"github.com/Mutiur03/pulse/internal/modules/identity/adapters/httpapi"
	"github.com/Mutiur03/pulse/internal/modules/identity/adapters/httpapi/oapi"
	"github.com/Mutiur03/pulse/internal/modules/identity/ports"
)

// Dependencies as interfaces, for callers and tests.
type (
	Accounts       = ports.Accounts
	OrgStore       = ports.OrgStore
	SessionRevoker = ports.SessionRevoker
	RoleStore      = ports.RoleStore
	// Limits throttle login, signup and password reset; any may be nil.
	Limits = ports.AuthLimits
)

// Deps are the module's dependencies; see httpapi.Handlers for each field.
type Deps = httpapi.Handlers

// Module is the identity module.
type Module struct{ handlers *httpapi.Handlers }

// New wires the module.
func New(d Deps) *Module {
	h := d
	return &Module{handlers: &h}
}

// Register mounts the module's routes on r.
func (m *Module) Register(r chi.Router) { oapi.HandlerFromMux(m.handlers, r) }
