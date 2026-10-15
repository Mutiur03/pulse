package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/mail"
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
		writeErr(w, http.StatusNotFound, "organization not found")
		return nil, false
	}
	if owner && role != "owner" {
		writeErr(w, http.StatusForbidden, "only organization owners can do this")
		return nil, false
	}
	return p, true
}

func (s *Server) ListMembers(w http.ResponseWriter, r *http.Request, org gen.OrgPath) {
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
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) RemoveMember(w http.ResponseWriter, r *http.Request, org gen.OrgPath, user int64) {
	if _, ok := orgAccess(w, r, org, true); !ok {
		return
	}
	err := s.Orgs.RemoveMember(r.Context(), org, user)
	switch {
	case errors.Is(err, orgs.ErrNotFound):
		writeErr(w, http.StatusNotFound, "member not found")
	case errors.Is(err, orgs.ErrLastOwner):
		writeErr(w, http.StatusConflict, "the organization needs at least one owner")
	case err != nil:
		internal(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func invite2gen(i orgs.Invite) gen.Invite {
	return gen.Invite{Id: i.ID, OrgId: i.OrgID, Email: i.Email, Role: gen.InviteRole(i.Role), CreatedAt: i.CreatedAt, ExpiresAt: i.ExpiresAt}
}

func (s *Server) ListInvites(w http.ResponseWriter, r *http.Request, org gen.OrgPath) {
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
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) CreateInvite(w http.ResponseWriter, r *http.Request, org gen.OrgPath) {
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
		writeErr(w, http.StatusBadRequest, "enter a valid email address")
		return
	}
	if body.Role != gen.InviteInputRoleOwner && body.Role != gen.InviteInputRoleMember {
		writeErr(w, http.StatusBadRequest, "role must be owner or member")
		return
	}
	inv, token, err := s.Orgs.CreateInvite(r.Context(), org, email, string(body.Role), p.User.ID)
	if errors.Is(err, orgs.ErrAlreadyMember) {
		writeErr(w, http.StatusConflict, "that person is already a member")
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
	s.send(mail.Message{To: email, Subject: p.User.Name + " invited you to " + orgName + " on Pulse",
		Text: p.User.Name + " (" + p.User.Email + ") invited you to join " + orgName + " on Pulse as " + inv.Role + ".\n\n" +
			"Accept the invite within 7 days:\n\n" + s.link("/invite", token) + "\n"})
	writeJSON(w, http.StatusCreated, invite2gen(inv))
}

func (s *Server) RevokeInvite(w http.ResponseWriter, r *http.Request, org gen.OrgPath, id int64) {
	if _, ok := orgAccess(w, r, org, true); !ok {
		return
	}
	err := s.Orgs.RevokeInvite(r.Context(), org, id)
	if errors.Is(err, orgs.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "invite not found")
		return
	} else if err != nil {
		internal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) LookupInvite(w http.ResponseWriter, r *http.Request) {
	var body gen.TokenRequest
	if !decode(w, r, &body) {
		return
	}
	if body.Token == "" || len(body.Token) > 100 {
		writeErr(w, http.StatusNotFound, "invite not found")
		return
	}
	i, err := s.Orgs.Lookup(r.Context(), body.Token)
	if errors.Is(err, orgs.ErrBadInvite) {
		writeErr(w, http.StatusNotFound, "the invite is invalid or has expired")
		return
	} else if err != nil {
		internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, gen.InviteInfo{OrgId: i.OrgID, OrgName: i.OrgName, Email: i.Email, Role: gen.InviteInfoRole(i.Role), Inviter: i.Inviter})
}

func (s *Server) AcceptInvite(w http.ResponseWriter, r *http.Request) {
	p := auth.FromContext(r.Context())
	if p == nil {
		writeErr(w, http.StatusUnauthorized, "not logged in")
		return
	}
	var body gen.TokenRequest
	if !decode(w, r, &body) {
		return
	}
	if body.Token == "" || len(body.Token) > 100 {
		writeErr(w, http.StatusNotFound, "invite not found")
		return
	}
	_, err := s.Orgs.Accept(r.Context(), body.Token, p.User.ID, p.User.Email)
	switch {
	case errors.Is(err, orgs.ErrBadInvite):
		writeErr(w, http.StatusNotFound, "the invite is invalid or has expired")
		return
	case errors.Is(err, orgs.ErrWrongUser):
		writeErr(w, http.StatusForbidden, "this invite was sent to a different email address")
		return
	case err != nil:
		internal(w, err)
		return
	}
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
	writeJSON(w, http.StatusOK, meOf(np))
}
