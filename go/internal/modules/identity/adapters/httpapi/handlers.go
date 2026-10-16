// Package httpapi serves the identity endpoints: accounts, sessions,
// password reset, email verification, organizations and invites. They set
// cookies and Retry-After headers, so they use the generated chi
// interface rather than the strict one.
package httpapi

import (
	"net/http"
	"time"

	"github.com/Mutiur03/pulse/internal/modules/identity/ports"

	"github.com/Mutiur03/pulse/internal/apikit"
	"github.com/Mutiur03/pulse/internal/modules/identity/adapters/httpapi/oapi"

	"github.com/Mutiur03/pulse/internal/audit"
	"github.com/Mutiur03/pulse/internal/clientip"
	"github.com/Mutiur03/pulse/internal/mail"
)

// Handlers implements oapi.ServerInterface.
type Handlers struct {
	Accounts ports.Accounts
	// Limits throttle login, signup and password reset.
	Limits ports.AuthLimits
	// ClientIP finds the client address behind trusted proxies; nil uses
	// the direct peer.
	ClientIP *clientip.Resolver
	// Sessions, Roles and Audit back the security
	// endpoints; nil disables each.
	Sessions ports.SessionRevoker
	Roles    ports.RoleStore
	Audit    audit.Recorder
	Orgs     ports.OrgStore
	// Mail sends reset, invite and verification emails; nil drops them.
	Mail mail.Sender
	// AppURL is the web app's public base URL used in email links.
	AppURL       string
	SecureCookie bool
	Now          func() time.Time
}

var _ oapi.ServerInterface = (*Handlers)(nil)

func internal(w http.ResponseWriter, err error) { apikit.WriteError(w, err) }
