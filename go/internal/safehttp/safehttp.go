// Package safehttp makes outgoing HTTP requests to user-supplied URLs (alert
// webhooks, Slack) without letting them reach internal networks. Every
// address is checked when the connection is dialed, after DNS resolution, so
// a hostname that resolves to a public address at save time and a private
// one later (DNS rebinding) is still refused. Redirects are re-checked too.
package safehttp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

// ErrBlocked is returned for URLs that point at a disallowed address.
var ErrBlocked = errors.New("destination address is not allowed")

// blockedRanges are never dialed unless allowlisted: private, loopback,
// link-local (including cloud metadata 169.254.169.254), carrier-grade NAT,
// benchmarking, IPv6 unique-local, NAT64 and the like.
var blockedRanges = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12",
		"192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "192.168.0.0/16", "198.18.0.0/15", "198.51.100.0/24",
		"203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4", "255.255.255.255/32",
		"::/128", "::1/128", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001::/23", "2001:db8::/32",
		"2002::/16", "fc00::/7", "fe80::/10", "fec0::/10", "ff00::/8",
	} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

// Policy decides which destinations are allowed.
type Policy struct {
	// AllowHosts are hostnames dialed without the address check, and
	// AllowNets are ranges that are allowed anyway. Both are meant for
	// development and tests (e.g. host.docker.internal).
	AllowHosts map[string]bool
	AllowNets  []netip.Prefix
	// Resolver looks up hostnames; nil uses net.DefaultResolver.
	Resolver interface {
		LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
	}
}

// ParseAllowlist reads a comma separated list of hostnames and CIDRs, as in
// the OUTBOUND_ALLOWLIST setting.
func ParseAllowlist(list string) (*Policy, error) {
	p := &Policy{AllowHosts: map[string]bool{}}
	for _, s := range strings.Split(list, ",") {
		s = strings.ToLower(strings.TrimSpace(s))
		switch {
		case s == "":
		case strings.Contains(s, "/"):
			pr, err := netip.ParsePrefix(s)
			if err != nil {
				return nil, fmt.Errorf("allowlist entry %q: %w", s, err)
			}
			p.AllowNets = append(p.AllowNets, pr.Masked())
		default:
			if a, err := netip.ParseAddr(s); err == nil {
				p.AllowNets = append(p.AllowNets, netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()))
			} else {
				p.AllowHosts[s] = true
			}
		}
	}
	return p, nil
}

// Blocked reports whether an address is in a disallowed range.
func Blocked(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsValid() || a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() ||
		a.IsMulticast() || a.IsUnspecified() || a.IsInterfaceLocalMulticast() {
		return true
	}
	for _, p := range blockedRanges {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

func (p *Policy) allowedAddr(a netip.Addr) bool {
	a = a.Unmap()
	if p != nil {
		for _, n := range p.AllowNets {
			if n.Contains(a) {
				return true
			}
		}
	}
	return !Blocked(a)
}

func (p *Policy) allowedHost(host string) bool {
	return p != nil && p.AllowHosts[strings.ToLower(strings.TrimSuffix(host, "."))]
}

func (p *Policy) lookup(ctx context.Context, host string) ([]netip.Addr, error) {
	if a, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		return []netip.Addr{a}, nil
	}
	if p != nil && p.Resolver != nil {
		return p.Resolver.LookupNetIP(ctx, "ip", host)
	}
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

// CheckURL validates a URL before it is saved: http(s), no credentials, and
// every address the host resolves to is allowed. Dial-time checks still
// apply when it is used.
func (p *Policy) CheckURL(ctx context.Context, raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return errors.New("target must be an http(s) URL")
	}
	if u.User != nil {
		return errors.New("credentials in the URL are not allowed")
	}
	host := u.Hostname()
	if p.allowedHost(host) {
		return nil
	}
	addrs, err := p.lookup(ctx, host)
	if err != nil {
		return fmt.Errorf("cannot resolve %s", host)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("cannot resolve %s", host)
	}
	for _, a := range addrs {
		if !p.allowedAddr(a) {
			return fmt.Errorf("%s: %w", host, ErrBlocked)
		}
	}
	return nil
}

// DialContext resolves the host itself and dials only allowed addresses,
// connecting to the checked IP so a second lookup cannot change it.
func (p *Policy) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	d := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	if p.allowedHost(host) {
		return d.DialContext(ctx, network, addr)
	}
	addrs, err := p.lookup(ctx, host)
	if err != nil {
		return nil, err
	}
	lastErr := fmt.Errorf("%s: %w", host, ErrBlocked)
	for _, a := range addrs {
		if !p.allowedAddr(a) {
			// One blocked address makes the whole host suspect.
			return nil, fmt.Errorf("%s: %w", host, ErrBlocked)
		}
	}
	for _, a := range addrs {
		c, err := d.DialContext(ctx, network, net.JoinHostPort(a.Unmap().String(), port))
		if err == nil {
			return c, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// MaxRedirects is how many redirects a client follows.
const MaxRedirects = 3

// Client returns an HTTP client that only reaches allowed addresses, ignores
// proxy settings, and follows at most MaxRedirects http(s) redirects.
func (p *Policy) Client(timeout time.Duration) *http.Client {
	tr := &http.Transport{
		Proxy:                 nil,
		DialContext:           p.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: timeout,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: tr,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= MaxRedirects {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return errors.New("redirect to a non-http URL")
			}
			// The dialer checks the new host's addresses.
			return nil
		},
	}
}
