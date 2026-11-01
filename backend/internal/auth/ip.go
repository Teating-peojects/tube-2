package auth

import (
	"net"
	"net/http"
	"strings"
)

// ClientIP returns the caller's IP. Forwarding headers are only honoured when
// trustProxy is set (the API must then be reachable only through the proxy,
// otherwise clients can spoof them to dodge rate limits).
//
//   - CF-Connecting-IP (Cloudflare) wins when present.
//   - X-Forwarded-For: the right-most valid entry is used, because that is the
//     one appended by our own proxy; left-most entries are client-controlled.
func ClientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if ip := parseIP(r.Header.Get("CF-Connecting-IP")); ip != "" {
			return ip
		}
		xff := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
		for i := len(xff) - 1; i >= 0; i-- {
			if ip := parseIP(xff[i]); ip != "" {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := parseIP(host); ip != "" {
		return ip
	}
	return "unknown"
}

func parseIP(s string) string {
	if ip := net.ParseIP(strings.TrimSpace(s)); ip != nil {
		return ip.String()
	}
	return ""
}

// IPPrefix truncates an address to /24 (IPv4) or /56 (IPv6), the only form the
// sessions table may store (data minimisation: a prefix is enough to spot
// "new network" without keeping a personal identifier). Returns nil if unparseable.
func IPPrefix(ip string) *string {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return nil
	}
	var n net.IPNet
	if v4 := parsed.To4(); v4 != nil {
		n = net.IPNet{IP: v4.Mask(net.CIDRMask(24, 32)), Mask: net.CIDRMask(24, 32)}
	} else {
		n = net.IPNet{IP: parsed.Mask(net.CIDRMask(56, 128)), Mask: net.CIDRMask(56, 128)}
	}
	s := n.String()
	return &s
}
