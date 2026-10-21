package httpapi

import (
	"net/http"
	"net/netip"
	"strings"
)

// clientKey identifies the client of r for rate limiting. X-Forwarded-For is believed only as far as trusted proxies
// appended it: walking from the nearest hop, the first address outside trusted is the client. IPv6 clients are keyed
// by their /64, which one subscriber usually controls entirely.
func clientKey(r *http.Request, trusted []netip.Prefix) string {
	addr, ok := parseHop(r.RemoteAddr)
	if !ok {
		return r.RemoteAddr
	}
	hops := forwardedHops(r.Header.Values("X-Forwarded-For"))
	for i := len(hops) - 1; i >= 0 && isTrusted(addr, trusted); i-- {
		next, ok := parseHop(hops[i])
		if !ok {
			break
		}
		addr = next
	}
	if addr.Is6() {
		p, _ := addr.WithZone("").Prefix(64) // cannot fail: a zoneless IPv6 address has 128 bits
		return p.String()
	}
	return addr.String()
}

// forwardedHops flattens X-Forwarded-For header lines into hops, nearest last.
func forwardedHops(values []string) []string {
	var hops []string
	for _, v := range values {
		for _, h := range strings.Split(v, ",") {
			hops = append(hops, strings.TrimSpace(h))
		}
	}
	return hops
}

// parseHop parses an address with or without a port.
func parseHop(s string) (netip.Addr, bool) {
	if ap, err := netip.ParseAddrPort(s); err == nil {
		return ap.Addr().Unmap(), true
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}, false
	}
	return a.Unmap(), true
}

func isTrusted(a netip.Addr, trusted []netip.Prefix) bool {
	for _, p := range trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}
