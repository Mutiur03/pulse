package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/audit"
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/httpx"
	"github.com/Mutiur03/pulse/internal/modules/identity/domain"
	"github.com/Mutiur03/pulse/internal/orgs"
)

// orgAccess writes 404 for non-members and, when owner is set, 403 for
// members who are not owners.
func orgAccess(w http.ResponseWriter, r *http.Request, org int64, owner bool) (*auth.Principal, bool) {
	p := auth.FromContext(r.Context())
	role := ""
	if p != nil {
		role = p.OrgRole(org)
	}
	if role == "" {
		httpx.WriteError(w, http.StatusNotFound, "organization not found")
		return nil, false
	}
	if owner && role != "owner" {
		httpx.WriteError(w, http.StatusForbidden, "only organization owners can do this")
		return nil, false
	}
	return p, true
}

func (s *Handlers) ListMembers(w http.ResponseWriter, r *http.Request, org gen.OrgPath) {
	if _, ok := orgAccess(w, r, org, false); !ok {
		return
	}
	list, err := s.Orgs.Members(r.Context(), org)
	if err != nil {
		internal(w, err)
		return
	}
	out := make([]gen.Member, 0, len(list))
	for _, m := range list {
		out = append(out, gen.Member{UserId: m.UserID, Email: m.Email, Name: m.Name, Role: gen.MemberRole(m.Role), JoinedAt: m.JoinedAt})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (s *Handlers) RemoveMember(w http.ResponseWriter, r *http.Request, org gen.OrgPath, user int64) {
	if _, ok := orgAccess(w, r, org, true); !ok {
		return
	}
	err := s.Orgs.RemoveMember(r.Context(), org, user)
	switch {
	case errors.Is(err, orgs.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "member not found")
	case errors.Is(err, orgs.ErrLastOwner):
		httpx.WriteError(w, http.StatusConflict, "the organization needs at least one owner")
	case err != nil:
		internal(w, err)
	default:
		s.record(r, audit.Entry{OrgID: org, Action: audit.MemberRemove, Target: "user " + strconv.FormatInt(user, 10)})
		w.WriteHeader(http.StatusNoContent)
	}
}

func invite2gen(i orgs.Invite) gen.Invite {
	return gen.Invite{Id: i.ID, OrgId: i.OrgID, Email: i.Email, Role: gen.InviteRole(i.Role), CreatedAt: i.CreatedAt, ExpiresAt: i.ExpiresAt}
}

func (s *Handlers) ListInvites(w http.ResponseWriter, r *http.Request, org gen.OrgPath) {
	if _, ok := orgAccess(w, r, org, true); !ok {
		return
	}
	list, err := s.Orgs.ListInvites(r.Context(), org)
	if err != nil {
		internal(w, err)
		return
	}
	out := make([]gen.Invite, 0, len(list))
	for _, i := range list {
		out = append(out, invite2gen(i))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (s *Handlers) CreateInvite(w http.ResponseWriter, r *http.Request, org gen.OrgPath) {
	p, ok := orgAccess(w, r, org, true)
	if !ok {
		return
	}
	var body gen.InviteInput
	if !decode(w, r, &body) {
		return
	}
	email := strings.TrimSpace(body.Email)
	if !auth.ValidEmail(email) {
		httpx.WriteError(w, http.StatusBadRequest, "enter a valid email address")
		return
	}
	if !domain.ValidRole(string(body.Role)) {
		httpx.WriteError(w, http.StatusBadRequest, "role must be owner or member")
		return
	}
	inv, token, err := s.Orgs.CreateInvite(r.Context(), org, email, string(body.Role), p.User.ID)
	if errors.Is(err, orgs.ErrAlreadyMember) {
		httpx.WriteError(w, http.StatusConflict, "that person is already a member")
		return
	} else if err != nil {
		internal(w, err)
		return
	}
	orgName := ""
	for _, o := range p.Orgs {
		if o.ID == org {
			orgName = o.Name
		}
	}
	s.send(domain.InviteMail(p.User, email, orgName, inv.Role, s.link("/invite", token)))
	s.record(r, audit.Entry{OrgID: org, Action: audit.InviteCreate, Target: email, Details: map[string]any{"role": inv.Role}})
	httpx.WriteJSON(w, http.StatusCreated, invite2gen(inv))
}

func (s *Handlers) RevokeInvite(w http.ResponseWriter, r *http.Request, org gen.OrgPath, id int64) {
	if _, ok := orgAccess(w, r, org, true); !ok {
		return
	}
	err := s.Orgs.RevokeInvite(r.Context(), org, id)
	if errors.Is(err, orgs.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "invite not found")
		return
	} else if err != nil {
		internal(w, err)
		return
	}
	s.record(r, audit.Entry{OrgID: org, Action: audit.InviteRevoke, Target: "invite " + strconv.FormatInt(id, 10)})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Handlers) LookupInvite(w http.ResponseWriter, r *http.Request) {
	var body gen.TokenRequest
	if !decode(w, r, &body) {
		return
	}
	if !domain.ValidToken(body.Token) {
		httpx.WriteError(w, http.StatusNotFound, "invite not found")
		return
	}
	i, err := s.Orgs.Lookup(r.Context(), body.Token)
	if errors.Is(err, orgs.ErrBadInvite) {
		httpx.WriteError(w, http.StatusNotFound, "the invite is invalid or has expired")
		return
	} else if err != nil {
		internal(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, gen.InviteInfo{OrgId: i.OrgID, OrgName: i.OrgName, Email: i.Email, Role: gen.InviteInfoRole(i.Role), Inviter: i.Inviter})
}

func (s *Handlers) AcceptInvite(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	if p == nil {
		httpx.WriteError(w, http.StatusUnauthorized, "not logged in")
		return
	}
	var body gen.TokenRequest
	if !decode(w, r, &body) {
		return
	}
	if !domain.ValidToken(body.Token) {
		httpx.WriteError(w, http.StatusNotFound, "invite not found")
		return
	}
	joined, err := s.Orgs.Accept(r.Context(), body.Token, p.User.ID, p.User.Email)
	switch {
	case errors.Is(err, orgs.ErrBadInvite):
		httpx.WriteError(w, http.StatusNotFound, "the invite is invalid or has expired")
		return
	case errors.Is(err, orgs.ErrWrongUser):
		httpx.WriteError(w, http.StatusForbidden, "this invite was sent to a different email address")
		return
	case err != nil:
		internal(w, err)
		return
	}
	s.record(r, audit.Entry{OrgID: joined, Action: audit.InviteAccept, Target: p.User.Email})
	c, err := r.Cookie(auth.CookieName)
	if err != nil {
		internal(w, err)
		return
	}
	np, err := s.Accounts.Principal(r.Context(), c.Value)
	if err != nil {
		internal(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, meOf(np))
}
