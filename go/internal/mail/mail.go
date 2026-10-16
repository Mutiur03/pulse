// Package mail sends transactional email (password resets, invites,
// verification) over SMTP, or logs it in development.
package mail

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// Message is a plain-text email.
type Message struct {
	To      string
	Subject string
	Text    string
}

// Sender delivers messages.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// Config selects and configures the sender. An empty Addr means log only.
type Config struct {
	Addr     string // host:port of the SMTP server
	From     string
	User     string
	Password string
}

// New returns an SMTP sender, or a LogSender when no server is configured.
func New(c Config) Sender {
	if c.Addr == "" {
		return LogSender{}
	}
	if c.From == "" {
		c.From = "Pulse <no-reply@pulse.local>"
	}
	return &SMTPSender{Config: c}
}

// LogSender writes emails to the log instead of sending them.
type LogSender struct{}

func (LogSender) Send(_ context.Context, m Message) error {
	slog.Info("email not sent, no SMTP_ADDR", "to", m.To, "subject", m.Subject, "text", m.Text)
	return nil
}

// SMTPSender sends through an SMTP server, using STARTTLS when offered and
// PLAIN auth when a user is configured.
type SMTPSender struct{ Config Config }

// Build renders the message with headers. Header values are checked for line
// breaks so user input cannot inject headers.
func Build(from string, m Message, now time.Time) ([]byte, error) {
	for _, v := range []string{from, m.To, m.Subject} {
		if strings.ContainsAny(v, "\r\n") {
			return nil, errors.New("mail: header contains a line break")
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\n", from, m.To, m.Subject, now.Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n")
	b.WriteString(strings.ReplaceAll(strings.ReplaceAll(m.Text, "\r\n", "\n"), "\n", "\r\n"))
	return []byte(b.String()), nil
}

func envelopeFrom(from string) string {
	if i := strings.LastIndex(from, "<"); i >= 0 {
		return strings.TrimSuffix(from[i+1:], ">")
	}
	return from
}

func (s *SMTPSender) Send(_ context.Context, m Message) error {
	body, err := Build(s.Config.From, m, time.Now())
	if err != nil {
		return err
	}
	var auth smtp.Auth
	if s.Config.User != "" {
		host, _, _ := net.SplitHostPort(s.Config.Addr)
		auth = smtp.PlainAuth("", s.Config.User, s.Config.Password, host)
	}
	return smtp.SendMail(s.Config.Addr, auth, envelopeFrom(s.Config.From), []string{m.To}, body)
}

// Async sends in the background so request latency does not depend on the
// mail server (or reveal whether an email was sent at all). Errors are logged.
func Async(s Sender, m Message) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.Send(ctx, m); err != nil {
			slog.Error("email failed", "to", m.To, "err", err)
		}
	}()
}
