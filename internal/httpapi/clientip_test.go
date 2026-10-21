package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestClientKey(t *testing.T) {
	proxies := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("fd00::/8"),
		netip.MustParsePrefix("fe80::/10")}
	cases := []struct {
		name    string
		remote  string
		xff     []string
		trusted []netip.Prefix
		want    string
	}{
		{"direct client", "192.0.2.10:1234", nil, proxies, "192.0.2.10"},
		{"header from an untrusted peer is ignored", "192.0.2.10:1234", []string{"203.0.113.5"}, proxies, "192.0.2.10"},
		{"no trusted proxies configured", "10.0.0.2:443", []string{"203.0.113.5"}, nil, "10.0.0.2"},
		{"behind a trusted proxy", "10.0.0.2:443", []string{"203.0.113.5"}, proxies, "203.0.113.5"},
		{"client-supplied hops are ignored", "10.0.0.2:443", []string{"198.51.100.7, 203.0.113.5, 10.0.0.3"}, proxies, "203.0.113.5"},
		{"hops across header lines", "10.0.0.2:443", []string{"198.51.100.7", "203.0.113.5"}, proxies, "203.0.113.5"},
		{"only trusted hops", "10.0.0.2:443", []string{"10.0.0.9"}, proxies, "10.0.0.9"},
		{"malformed hop stops the walk", "10.0.0.2:443", []string{"203.0.113.5, bogus"}, proxies, "10.0.0.2"},
		{"hop with a port", "10.0.0.2:443", []string{"203.0.113.5:4711"}, proxies, "203.0.113.5"},
		{"IPv4-mapped peer", "[::ffff:192.0.2.10]:1234", nil, proxies, "192.0.2.10"},
		{"IPv6 client keyed by its /64", "[2001:db8:1:2:3:4:5:6]:1234", nil, proxies, "2001:db8:1:2::/64"},
		{"IPv6 client behind an IPv6 proxy", "[fd00::1]:443", []string{"2001:db8:aa:bb::9"}, proxies, "2001:db8:aa:bb::/64"},
		{"scoped trusted IPv6 peer", "[fe80::1%eth0]:443", []string{"203.0.113.5"}, proxies, "203.0.113.5"},
		{"scoped IPv6 client keyed by its /64", "[fe80::1:2:3:4%eth0]:1234", nil, nil, "fe80::/64"},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = c.remote
		for _, v := range c.xff {
			r.Header.Add("X-Forwarded-For", v)
		}
		if got := clientKey(r, c.trusted); got != c.want {
			t.Errorf("%s: clientKey = %q, want %q", c.name, got, c.want)
		}
	}
}
