package alerts

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Mutiur03/pulse/internal/mail"
)

// ChannelKind is how a notification is delivered.
type ChannelKind string

const (
	Email   ChannelKind = "email"
	Webhook ChannelKind = "webhook"
	Slack   ChannelKind = "slack"
)

// Channel is a notification destination. Target is an email address or URL;
// Secret signs webhook bodies.
type Channel struct {
	ID        int64
	ProjectID int64
	Name      string
	Kind      ChannelKind
	Target    string
	Secret    string
	CreatedAt time.Time
}

// Validate checks a channel before it is saved.
func (c *Channel) Validate() error {
	c.Name, c.Target = strings.TrimSpace(c.Name), strings.TrimSpace(c.Target)
	if c.Name == "" || len(c.Name) > 200 {
		return errors.New("name must be 1 to 200 characters")
	}
	switch c.Kind {
	case Email:
		if !strings.Contains(c.Target, "@") || strings.ContainsAny(c.Target, " \r\n<>,") || len(c.Target) > 254 {
			return errors.New("target must be an email address")
		}
	case Webhook, Slack:
		u, err := url.Parse(c.Target)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(c.Target) > 2000 {
			return errors.New("target must be an http(s) URL")
		}
	default:
		return fmt.Errorf("unknown channel kind %q", c.Kind)
	}
	return nil
}

// Payload is the JSON body sent to webhooks; email and Slack render it as text.
type Payload struct {
	Test       bool       `json:"test,omitempty"`
	Notice     Notice     `json:"notice"`
	RuleID     int64      `json:"rule_id"`
	RuleName   string     `json:"rule_name"`
	ProjectID  int64      `json:"project_id"`
	State      State      `json:"state"`
	Previous   State      `json:"previous_state"`
	Metric     Metric     `json:"metric"`
	Condition  string     `json:"condition"`
	Value      *float64   `json:"value,omitempty"`
	Threshold  float64    `json:"threshold"`
	Comparison Comparison `json:"comparison"`
	WindowSec  int64      `json:"window_seconds"`
	IssueID    int64      `json:"issue_id,omitempty"`
	TraceID    string     `json:"trace_id,omitempty"`
	Title      string     `json:"title,omitempty"`
	URL        string     `json:"url"`
	At         time.Time  `json:"at"`
}

// Summary is the one-line subject used by email and Slack.
func (p Payload) Summary() string {
	prefix := map[Notice]string{NoticeFiring: "[Alerting]", NoticeRepeat: "[Still alerting]", NoticeResolved: "[Resolved]"}[p.Notice]
	if p.Test {
		prefix = "[Test]"
	}
	if prefix == "" {
		prefix = "[" + string(p.State) + "]"
	}
	return prefix + " " + p.RuleName
}

// Text is the plain-text body used by email and Slack.
func (p Payload) Text(appURL string) string {
	var b strings.Builder
	if p.Test {
		b.WriteString("This is a test notification from Pulse. Alerts for this project will be sent here.\n")
		return b.String()
	}
	fmt.Fprintf(&b, "%s\n\nCondition: %s\n", p.Summary(), p.Condition)
	if p.Value != nil {
		fmt.Fprintf(&b, "Value: %s\n", FormatValue(p.Metric, *p.Value))
	}
	if p.Title != "" {
		fmt.Fprintf(&b, "Issue: %s\n", p.Title)
	}
	fmt.Fprintf(&b, "State: %s (was %s) at %s\n", p.State, p.Previous, p.At.UTC().Format(time.RFC1123))
	if appURL != "" {
		fmt.Fprintf(&b, "\nOpen the alert: %s\n", p.URL)
		if p.IssueID > 0 {
			fmt.Fprintf(&b, "Issue: %s/issues/%d\n", appURL, p.IssueID)
		}
		if p.TraceID != "" {
			fmt.Fprintf(&b, "Example trace: %s/traces/%s\n", appURL, p.TraceID)
		}
	}
	return b.String()
}

// SignatureHeader carries the webhook signature: "t=<unix>,v1=<hex>", where
// v1 is HMAC-SHA256(secret, "<unix>.<body>").
const SignatureHeader = "X-Pulse-Signature"

// Sign returns the signature header value for body at t.
func Sign(secret string, body []byte, t time.Time) string {
	ts := strconv.FormatInt(t.Unix(), 10)
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(ts + "."))
	m.Write(body)
	return "t=" + ts + ",v1=" + hex.EncodeToString(m.Sum(nil))
}

// Verify checks a signature header, rejecting ones older than tolerance.
func Verify(secret string, body []byte, header string, now time.Time, tolerance time.Duration) bool {
	var ts, sig string
	for _, part := range strings.Split(header, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch k {
		case "t":
			ts = v
		case "v1":
			sig = v
		}
	}
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || sig == "" {
		return false
	}
	at := time.Unix(sec, 0)
	if now.Sub(at) > tolerance || at.Sub(now) > tolerance {
		return false
	}
	want := Sign(secret, body, at)
	return hmac.Equal([]byte(want), []byte("t="+ts+",v1="+sig))
}

// Notifier delivers payloads to channels.
type Notifier struct {
	Mail   mail.Sender
	HTTP   *http.Client
	AppURL string
	Now    func() time.Time
}

func (n *Notifier) now() time.Time {
	if n.Now != nil {
		return n.Now()
	}
	return time.Now()
}

func (n *Notifier) client() *http.Client {
	if n.HTTP != nil {
		return n.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// Send delivers one payload to one channel.
func (n *Notifier) Send(ctx context.Context, c Channel, p Payload) error {
	switch c.Kind {
	case Email:
		if n.Mail == nil {
			return errors.New("email is not configured")
		}
		return n.Mail.Send(ctx, mail.Message{To: c.Target, Subject: p.Summary(), Text: p.Text(n.AppURL)})
	case Webhook:
		body, err := json.Marshal(p)
		if err != nil {
			return err
		}
		return n.post(ctx, c.Target, body, map[string]string{SignatureHeader: Sign(c.Secret, body, n.now()), "X-Pulse-Event": string(p.Notice)})
	case Slack:
		text := "*" + p.Summary() + "*\n" + p.Text(n.AppURL)
		if p.Test {
			text = p.Text(n.AppURL)
		}
		body, err := json.Marshal(map[string]string{"text": text})
		if err != nil {
			return err
		}
		return n.post(ctx, c.Target, body, nil)
	}
	return fmt.Errorf("unknown channel kind %q", c.Kind)
}

func (n *Notifier) post(ctx context.Context, target string, body []byte, headers map[string]string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Pulse-Alerts/1")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := n.client().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	if res.StatusCode >= 300 {
		return fmt.Errorf("%s answered HTTP %d", req.URL.Host, res.StatusCode)
	}
	return nil
}
