package keys

import (
	"errors"
	"net/url"
	"strings"
)

// MaxOrigins caps the allowed origins of a project.
const MaxOrigins = 50

// CleanOrigin validates and normalizes an allowed origin: scheme://host[:port]
// with an optional leading "*." wildcard label, e.g. https://*.example.com.
func CleanOrigin(s string) (string, error) {
	s = strings.TrimRight(strings.TrimSpace(s), "/")
	bad := errors.New("origins look like https://app.example.com or https://*.example.com")
	if s == "" || len(s) > 255 || strings.ContainsAny(s, " \t\r\n") {
		return "", bad
	}
	scheme, rest, ok := strings.Cut(s, "://")
	if !ok || (scheme != "http" && scheme != "https") {
		return "", bad
	}
	host := rest
	wild := strings.HasPrefix(host, "*.")
	if wild {
		host = host[2:]
	}
	u, err := url.Parse(scheme + "://" + host)
	if err != nil || u.Host != host || u.Hostname() == "" || strings.Contains(u.Hostname(), "*") {
		return "", bad
	}
	return strings.ToLower(s), nil
}

// CleanOrigins validates a list, dropping duplicates.
func CleanOrigins(in []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, o := range in {
		if strings.TrimSpace(o) == "" {
			continue
		}
		c, err := CleanOrigin(o)
		if err != nil {
			return nil, err
		}
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	if len(out) > MaxOrigins {
		return nil, errors.New("too many origins")
	}
	return out, nil
}

// OriginAllowed reports whether a browser Origin header value may use a key
// with the given allowed origins. An empty list allows every origin.
func OriginAllowed(allowed []string, origin string) bool {
	if len(allowed) == 0 {
		return true
	}
	origin = strings.ToLower(strings.TrimRight(origin, "/"))
	scheme, host, ok := strings.Cut(origin, "://")
	if !ok || host == "" {
		return false
	}
	for _, a := range allowed {
		if a == origin {
			return true
		}
		as, ah, _ := strings.Cut(a, "://")
		if as != scheme || !strings.HasPrefix(ah, "*.") {
			continue
		}
		// *.example.com matches sub.example.com (with the same port), not
		// example.com itself.
		if suffix := ah[1:]; strings.HasSuffix(host, suffix) && len(host) > len(suffix) {
			return true
		}
	}
	return false
}
