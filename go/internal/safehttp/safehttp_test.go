package safehttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestBlocked(t *testing.T) {
	for _, s := range []string{
		"127.0.0.1", "10.1.2.3", "172.16.5.4", "192.168.1.1", "169.254.169.254", "100.64.0.1", "0.0.0.0",
		"::1", "fd00::1", "fe80::1", "::ffff:127.0.0.1", "::ffff:169.254.169.254", "64:ff9b::a9fe:a9fe", "224.0.0.1",
	} {
		if !Blocked(netip.MustParseAddr(s)) {
			t.Errorf("%s not blocked", s)
		}
	}
	for _, s := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111", "93.184.216.34"} {
		if Blocked(netip.MustParseAddr(s)) {
			t.Errorf("%s blocked", s)
		}
	}
}

// fakeDNS answers lookups from a map and counts them.
type fakeDNS struct {
	answers map[string][]string
	calls   int
}

func (f *fakeDNS) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	f.calls++
	var out []netip.Addr
	for _, s := range f.answers[host] {
		out = append(out, netip.MustParseAddr(s))
	}
	if len(out) == 0 {
		return nil, errors.New("no such host")
	}
	return out, nil
}

func TestCheckURL(t *testing.T) {
	dns := &fakeDNS{answers: map[string][]string{
		"public.test":   {"93.184.216.34"},
		"internal.test": {"10.0.0.5"},
		"mixed.test":    {"93.184.216.34", "127.0.0.1"},
	}}
	p := &Policy{Resolver: dns, AllowHosts: map[string]bool{"host.docker.internal": true}}
	ctx := context.Background()
	for _, ok := range []string{"https://public.test/hook", "http://host.docker.internal:9999/x", "https://8.8.8.8/"} {
		if err := p.CheckURL(ctx, ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{
		"https://internal.test/", "https://mixed.test/", "http://127.0.0.1:8081/", "http://169.254.169.254/latest/meta-data/",
		"http://[::1]/", "http://[fd12::1]/", "ftp://public.test/", "https://user:pw@public.test/", "https://nowhere.test/",
	} {
		if err := p.CheckURL(ctx, bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestDialTimeCheckBeatsRebinding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	port := srv.URL[strings.LastIndex(srv.URL, ":"):]
	// Saved while public, now resolving to loopback where the server is.
	dns := &fakeDNS{answers: map[string][]string{"rebind.test": {"127.0.0.1"}}}
	p := &Policy{Resolver: dns}
	_, err := p.Client(2 * time.Second).Get("http://rebind.test" + port + "/")
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("got %v", err)
	}
	// Allowlisting the range lets it through (dev and e2e setups).
	p.AllowNets = []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}
	res, err := p.Client(2 * time.Second).Get("http://rebind.test" + port + "/")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
}

func TestRedirectToBlockedHost(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://metadata.test"+target.URL[strings.LastIndex(target.URL, ":"):]+"/", http.StatusFound)
	}))
	defer redirector.Close()
	// The redirector is allowed (loopback is allowlisted here); the redirect
	// target resolves to the metadata address and is not.
	p := &Policy{
		Resolver:  &fakeDNS{answers: map[string][]string{"metadata.test": {"169.254.169.254"}, "hooks.test": {"127.0.0.1"}}},
		AllowNets: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")},
	}
	_, err := p.Client(2 * time.Second).Get("http://hooks.test" + redirector.URL[strings.LastIndex(redirector.URL, ":"):] + "/")
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("got %v", err)
	}
}

func TestParseAllowlist(t *testing.T) {
	p, err := ParseAllowlist("host.docker.internal, 172.17.0.0/16, 10.0.0.9")
	if err != nil {
		t.Fatal(err)
	}
	if !p.allowedHost("HOST.docker.internal") || !p.allowedAddr(netip.MustParseAddr("172.17.0.1")) ||
		!p.allowedAddr(netip.MustParseAddr("10.0.0.9")) || p.allowedAddr(netip.MustParseAddr("10.0.0.8")) {
		t.Fatalf("%+v", p)
	}
	if _, err := ParseAllowlist("10.0.0.0/99"); err == nil {
		t.Fatal("bad CIDR accepted")
	}
}
