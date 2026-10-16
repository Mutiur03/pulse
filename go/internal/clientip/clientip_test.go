package clientip

import (
	"net/http/httptest"
	"testing"
)

func TestIP(t *testing.T) {
	r, err := Parse("10.0.0.0/8, 127.0.0.1, ::1")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, peer, xff, want string
	}{
		{"untrusted peer ignores header", "203.0.113.9:5000", "1.2.3.4", "203.0.113.9"},
		{"trusted peer uses last hop", "10.1.2.3:5000", "198.51.100.7", "198.51.100.7"},
		{"spoofed left entries are skipped", "10.1.2.3:5000", "1.1.1.1, 198.51.100.7", "198.51.100.7"},
		{"chain of trusted proxies", "127.0.0.1:1", "198.51.100.7, 10.9.9.9", "198.51.100.7"},
		{"trusted peer without header", "10.1.2.3:5000", "", "10.1.2.3"},
		{"ipv6 peer", "[::1]:80", "2001:db8::5", "2001:db8::5"},
		{"garbage hop stops the walk", "10.1.2.3:5000", "1.1.1.1, nonsense", "10.1.2.3"},
		{"mapped ipv4", "[::ffff:10.0.0.2]:1", "198.51.100.8", "198.51.100.8"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = c.peer
			if c.xff != "" {
				req.Header.Set("X-Forwarded-For", c.xff)
			}
			if got := r.IP(req); got != c.want {
				t.Fatalf("got %s, want %s", got, c.want)
			}
		})
	}
}

func TestNoTrustedProxies(t *testing.T) {
	var r *Resolver
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:1"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	if got := r.IP(req); got != "10.0.0.1" {
		t.Fatal(got)
	}
	if _, err := Parse("10.0.0.0/33"); err == nil {
		t.Fatal("bad CIDR accepted")
	}
}
