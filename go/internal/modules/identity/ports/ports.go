// Package ports declares what the identity module reads and writes.
package ports

import (
	"context"

	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/orgs"
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

// AuthLimits are the per-address and per-account limits on the public auth
// endpoints. Any of them may be nil.
type AuthLimits struct {
	// LoginFail locks an email from one address after repeated failures.
	LoginFail *auth.Throttle
	// LoginEmail locks an email after failures from any addresses, so
	// rotating addresses does not give unlimited guesses.
	LoginEmail *auth.Throttle
	// LoginIP, SignupIP and ResetIP count every attempt per address.
	LoginIP  *auth.Throttle
	SignupIP *auth.Throttle
	ResetIP  *auth.Throttle
	// ResetEmail limits reset emails per address and email.
	ResetEmail *auth.Throttle
}

// SessionRevoker ends all sessions of a user.
type SessionRevoker interface {
	LogoutAll(ctx context.Context, userID int64) (int64, error)
}

// RoleStore changes organization roles.
type RoleStore interface {
	SetRole(ctx context.Context, org, user int64, role string) error
}
