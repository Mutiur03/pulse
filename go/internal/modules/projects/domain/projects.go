// Package domain holds the project API rules. Project and key storage live
// in internal/projects and internal/keys, shared with the ingest gateway.
package domain

import (
	"errors"

	"github.com/Mutiur03/pulse/internal/auth"
)

// Errors the service reports.
var (
	ErrNotOrgOwner = errors.New("only organization owners can create projects")
	ErrOriginsOff  = errors.New("origins are not configured")
)

// DefaultOrg picks the organization for a new project when none is named:
// none for users without organizations, else the first one, else the
// first one the user owns. ok is false when the user owns none.
func DefaultOrg(p *auth.Principal) (int64, bool) {
	if len(p.Orgs) == 0 || p.OrgRole(p.Orgs[0].ID) == "owner" {
		return 0, true
	}
	for _, o := range p.Orgs {
		if o.Role == "owner" {
			return o.ID, true
		}
	}
	return 0, false
}

// KeyLabel shortens a public key for the audit log.
func KeyLabel(k string) string {
	if len(k) > 12 {
		return k[:12] + "..."
	}
	return k
}
