// Package clientip finds the real client address of a request. Forwarded
// headers are only believed when the direct peer is a trusted proxy.
package clientip

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// Resolver walks X-Forwarded-For from the right, skipping trusted proxies,
// and returns the first address that is not one. With no trusted proxies it
// always returns the direct peer.
type Resolver struct {
	Trusted []netip.Prefix
}

// Parse reads a comma separated list of CIDRs or single addresses, as in the
// TRUSTED_PROXIES setting.
func Parse(list string) (*Resolver, error) {
	r := &Resolver{}
	for _, s := range strings.Split(list, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if !strings.Contains(s, "/") {
			a, err := netip.ParseAddr(s)
			if err != nil {
				return nil, fmt.Errorf("trusted proxy %q: %w", s, err)
			}
			r.Trusted = append(r.Trusted, netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()))
			continue
		}
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q: %w", s, err)
		}
		r.Trusted = append(r.Trusted, p.Masked())
	}
	return r, nil
}

func (r *Resolver) trusted(a netip.Addr) bool {
	if r == nil {
		return false
	}
	a = a.Unmap()
	for _, p := range r.Trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

func parse(s string) (netip.Addr, bool) {
	s = strings.TrimSpace(s)
	if h, _, err := net.SplitHostPort(s); err == nil {
		s = h
	}
	s = strings.Trim(s, "[]")
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}, false
	}
	return a.Unmap(), true
}

// IP returns the client address of the request as a string.
func (r *Resolver) IP(req *http.Request) string {
	peer, ok := parse(req.RemoteAddr)
	if !ok {
		return req.RemoteAddr
	}
	if !r.trusted(peer) {
		return peer.String()
	}
	var hops []string
	for _, h := range req.Header.Values("X-Forwarded-For") {
		hops = append(hops, strings.Split(h, ",")...)
	}
	client := peer
	for i := len(hops) - 1; i >= 0; i-- {
		a, ok := parse(hops[i])
		if !ok {
			// A garbled hop cannot be trusted further; stop at the last good one.
			break
		}
		client = a
		if !r.trusted(a) {
			break
		}
	}
	return client.String()
}
