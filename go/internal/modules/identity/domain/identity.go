// Package domain holds the identity rules: roles, invite tokens and the
// account emails. Users, sessions and organizations are stored by
// internal/auth and internal/orgs, which the auth middleware also uses.
package domain

import (
	"github.com/Mutiur03/pulse/internal/auth"
	"github.com/Mutiur03/pulse/internal/mail"
)

// ValidRole reports whether role is an organization role.
func ValidRole(role string) bool { return role == "owner" || role == "member" }

// ValidToken reports whether an invite token is worth looking up.
func ValidToken(token string) bool { return token != "" && len(token) <= 100 }

// Truncate cuts s to at most n bytes.
func Truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// VerificationMail asks u to confirm their address at link.
func VerificationMail(u auth.User, link string) mail.Message {
	return mail.Message{To: u.Email, Subject: "Confirm your email for Pulse", Text: "Hi " + u.Name + ",\n\n" +
		"Confirm your email address by opening this link:\n\n" + link + "\n\n" +
		"If you did not create a Pulse account, you can ignore this email.\n"}
}

// ResetMail sends u a password reset link.
func ResetMail(u auth.User, link string) mail.Message {
	return mail.Message{To: u.Email, Subject: "Reset your Pulse password", Text: "Hi " + u.Name + ",\n\n" +
		"Someone asked to reset the password for your Pulse account. Open this link within an hour to choose a new one:\n\n" +
		link + "\n\n" +
		"If it was not you, ignore this email; your password stays the same.\n"}
}

// InviteMail invites email to org on behalf of inviter.
func InviteMail(inviter auth.User, email, orgName, role, link string) mail.Message {
	return mail.Message{To: email, Subject: inviter.Name + " invited you to " + orgName + " on Pulse",
		Text: inviter.Name + " (" + inviter.Email + ") invited you to join " + orgName + " on Pulse as " + role + ".\n\n" +
			"Accept the invite within 7 days:\n\n" + link + "\n"}
}
