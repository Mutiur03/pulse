// Package api implements the query API defined in schemas/openapi/api.yaml.
package api

import (
	"context"
	"net/http"
	"time"

	"github.com/Mutiur03/pulse/internal/api/legacy"
	"github.com/Mutiur03/pulse/internal/apikit"
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/keys"
	"github.com/Mutiur03/pulse/internal/orgs"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/audit"
	"github.com/Mutiur03/pulse/internal/clientip"
	"github.com/Mutiur03/pulse/internal/httpx"
	"github.com/Mutiur03/pulse/internal/mail"
)

// Accounts logs users in and out.
type Accounts interface {
	Login(ctx context.Context, email, password, userAgent string) (string, auth.User, error)
	Signup(ctx context.Context, email, password, name, userAgent, invite string) (string, auth.User, error)
	RequestReset(ctx context.Context, email string) (string, auth.User, bool, error)
	ResetPassword(ctx context.Context, token, password string) error
	IssueVerify(ctx context.Context, userID int64) (string, error)
	VerifyEmail(ctx context.Context, token string) error
	Principal(ctx context.Context, token string) (*auth.Principal, error)
	Logout(ctx context.Context, token string) error
}

// KeyStore manages ingest keys; Purge drops revoked keys from the gateway cache.
type KeyStore interface {
	List(ctx context.Context, projectID int64) ([]keys.APIKey, error)
	Rotate(ctx context.Context, projectID int64) (keys.APIKey, []string, error)
	Create(ctx context.Context, projectID int64) (keys.APIKey, error)
	Revoke(ctx context.Context, projectID, keyID int64) (keys.APIKey, error)
	Purge(ctx context.Context, publicKey string) error
}

// ProjectStore creates, lists and renames projects.
type ProjectStore interface {
	List(ctx context.Context, userID int64) ([]auth.Project, error)
	Create(ctx context.Context, userID, orgID int64, name string) (auth.Project, keys.APIKey, error)
	Rename(ctx context.Context, projectID int64, name string) (string, error)
}

// OrgStore manages organization members and invites.
type OrgStore interface {
	Members(ctx context.Context, org int64) ([]orgs.Member, error)
	RemoveMember(ctx context.Context, org, user int64) error
	CreateInvite(ctx context.Context, org int64, email, role string, invitedBy int64) (orgs.Invite, string, error)
	ListInvites(ctx context.Context, org int64) ([]orgs.Invite, error)
	RevokeInvite(ctx context.Context, org, id int64) error
	Lookup(ctx context.Context, token string) (orgs.InviteInfo, error)
	Accept(ctx context.Context, token string, userID int64, email string) (int64, error)
}

type Server struct {
	Accounts Accounts
	Keys     KeyStore
	Projects ProjectStore
	// Limits throttle login, signup and password reset.
	Limits AuthLimits
	// ClientIP finds the client address behind trusted proxies; nil uses
	// the direct peer.
	ClientIP *clientip.Resolver
	// Sessions, Roles, Origins, Audit and AuditLog back the security
	// endpoints; nil disables each.
	Sessions SessionRevoker
	Roles    RoleStore
	Origins  OriginStore
	Audit    audit.Recorder
	AuditLog AuditReader
	Orgs     OrgStore
	// Mail sends reset, invite and verification emails; nil drops them.
	Mail mail.Sender
	// AppURL is the web app's public base URL used in email links.
	AppURL string
	// Governance keeps data settings and deletion requests; nil disables
	// them. Persons links a deletion to every id of the person.
	Governance   GovernanceStore
	Persons      PersonLinker
	SecureCookie bool
	Now          func() time.Time
}

var _ legacy.ServerInterface = (*Server)(nil)

func writeJSON(w http.ResponseWriter, code int, v interface{}) { httpx.WriteJSON(w, code, v) }

func writeErr(w http.ResponseWriter, code int, msg string) { httpx.WriteError(w, code, msg) }

func internal(w http.ResponseWriter, err error) { apikit.WriteError(w, err) }

func limit(l *int, def, max int) int { return apikit.Limit(l, def, max) }

func (s *Server) GetHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, gen.Health{Status: "healthy", Service: "query-api"})
}

func canRead(r *http.Request, project int64) bool { return apikit.CanRead(r.Context(), project) }
