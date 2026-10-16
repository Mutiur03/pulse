// Package governance holds per-project data rules: PII scrubbing applied at
// ingest, retention periods enforced by the governance worker, and deletion
// requests for one person's data.
package governance

import (
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strings"
)

// Rules are a project's scrubbing settings.
type Rules struct {
	Emails bool
	Cards  bool
	IPs    bool
	// Keys are property or attribute names whose values are always removed;
	// a name matches when it contains a key (case-insensitive).
	Keys []string
	// Patterns are RE2 expressions; matches are replaced in every string.
	Patterns []string
}

// DefaultKeys are scrubbed unless a project changes them.
var DefaultKeys = []string{"password", "passwd", "secret", "token", "api_key", "apikey", "authorization", "cookie", "card_number", "cvv", "ssn"}

// DefaultRules apply to projects without saved settings.
func DefaultRules() Rules {
	return Rules{Cards: true, Keys: append([]string{}, DefaultKeys...), Patterns: []string{}}
}

// Limits on custom rules.
const (
	MaxKeys       = 50
	MaxKeyLength  = 100
	MaxPatterns   = 20
	MaxPatternLen = 200
)

// Replacement markers.
const (
	Redacted = "[redacted]"
	EmailTag = "[email]"
	CardTag  = "[card]"
	IPTag    = "[ip]"
)

// Clean trims and validates the rules, dropping empty and duplicate entries.
func (r Rules) Clean() (Rules, error) {
	keys := []string{}
	seen := map[string]bool{}
	for _, k := range r.Keys {
		k = strings.ToLower(strings.TrimSpace(k))
		if k == "" || seen[k] {
			continue
		}
		if len(k) > MaxKeyLength {
			return r, fmt.Errorf("keys must be at most %d characters", MaxKeyLength)
		}
		seen[k] = true
		keys = append(keys, k)
	}
	if len(keys) > MaxKeys {
		return r, fmt.Errorf("at most %d keys", MaxKeys)
	}
	pats := []string{}
	for _, p := range r.Patterns {
		if strings.TrimSpace(p) == "" {
			continue
		}
		if len(p) > MaxPatternLen {
			return r, fmt.Errorf("patterns must be at most %d characters", MaxPatternLen)
		}
		if _, err := regexp.Compile(p); err != nil {
			return r, fmt.Errorf("pattern %q: %v", p, err)
		}
		pats = append(pats, p)
	}
	if len(pats) > MaxPatterns {
		return r, fmt.Errorf("at most %d patterns", MaxPatterns)
	}
	r.Keys, r.Patterns = keys, pats
	return r, nil
}

var (
	reEmail = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	// Runs of 13 to 19 digits, optionally split by spaces or dashes; only
	// numbers passing the Luhn check are cards.
	reCard = regexp.MustCompile(`\b\d(?:[ \-]?\d){12,18}\b`)
	reIPv4 = regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)\.){3}(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)\b`)
	// IPv6 candidates are checked with net.ParseIP.
	reIPv6 = regexp.MustCompile(`[0-9A-Fa-f]{0,4}(?::[0-9A-Fa-f]{0,4}){2,7}`)
)

// Scrubber applies compiled rules. A nil Scrubber changes nothing.
type Scrubber struct {
	rules    Rules
	patterns []*regexp.Regexp
}

// Compile prepares rules for use.
func Compile(r Rules) (*Scrubber, error) {
	r, err := r.Clean()
	if err != nil {
		return nil, err
	}
	s := &Scrubber{rules: r}
	for _, p := range r.Patterns {
		s.patterns = append(s.patterns, regexp.MustCompile(p))
	}
	return s, nil
}

// Rules returns the cleaned rules.
func (s *Scrubber) Rules() Rules { return s.rules }

func luhn(digits string) bool {
	sum, double := 0, false
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i] - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return sum%10 == 0
}

// String scrubs one value.
func (s *Scrubber) String(v string) string {
	if s == nil || v == "" {
		return v
	}
	for _, re := range s.patterns {
		v = re.ReplaceAllString(v, Redacted)
	}
	if s.rules.Emails && strings.Contains(v, "@") {
		v = reEmail.ReplaceAllString(v, EmailTag)
	}
	if s.rules.Cards {
		v = reCard.ReplaceAllStringFunc(v, func(m string) string {
			d := strings.NewReplacer(" ", "", "-", "").Replace(m)
			if len(d) >= 13 && len(d) <= 19 && luhn(d) {
				return CardTag
			}
			return m
		})
	}
	if s.rules.IPs {
		v = reIPv4.ReplaceAllString(v, IPTag)
		if strings.Count(v, ":") >= 2 {
			v = reIPv6.ReplaceAllStringFunc(v, func(m string) string {
				if strings.Contains(m, ":") && strings.ContainsAny(m, "0123456789abcdefABCDEF") && net.ParseIP(m) != nil {
					return IPTag
				}
				return m
			})
		}
	}
	return v
}

// SensitiveKey reports whether a property name's value is always removed.
func (s *Scrubber) SensitiveKey(key string) bool {
	if s == nil {
		return false
	}
	k := strings.ToLower(key)
	for _, x := range s.rules.Keys {
		if strings.Contains(k, x) {
			return true
		}
	}
	return false
}

// Value scrubs a decoded JSON value in place and returns it.
func (s *Scrubber) Value(v interface{}) interface{} {
	if s == nil {
		return v
	}
	switch x := v.(type) {
	case string:
		return s.String(x)
	case map[string]interface{}:
		s.Map(x)
		return x
	case []interface{}:
		for i := range x {
			x[i] = s.Value(x[i])
		}
		return x
	}
	return v
}

// keepKeys hold identifiers needed to link a person's ids; they are not
// scrubbed so identify and alias keep working.
var keepKeys = map[string]bool{"$anon_distinct_id": true, "alias": true, "$session_id": true}

// Map scrubs every value of an object, removing sensitive keys' values.
func (s *Scrubber) Map(m map[string]interface{}) {
	if s == nil {
		return
	}
	for k, v := range m {
		switch {
		case keepKeys[k]:
		case s.SensitiveKey(k):
			if v != nil {
				m[k] = Redacted
			}
		default:
			m[k] = s.Value(v)
		}
	}
}

// StringMap scrubs string attributes such as span attributes.
func (s *Scrubber) StringMap(m map[string]string) {
	if s == nil {
		return
	}
	for k, v := range m {
		if s.SensitiveKey(k) {
			m[k] = Redacted
		} else {
			m[k] = s.String(v)
		}
	}
}

// JSON scrubs an encoded JSON document. Text that is not JSON is scrubbed
// as a string.
func (s *Scrubber) JSON(raw []byte) []byte {
	if s == nil || len(raw) == 0 {
		return raw
	}
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return []byte(s.String(string(raw)))
	}
	out, err := json.Marshal(s.Value(v))
	if err != nil {
		return raw
	}
	return out
}
