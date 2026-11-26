package sentry

import (
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// These mirror Relay's localhost inbound filter (relay-filter/src/localhost.rs),
// so that the SDK and Relay classify the same request alike. Do not extend them.
var (
	localhostIPs     = []string{"127.0.0.1", "::1"}
	localhostDomains = []string{"127.0.0.1", "localhost"}
)

// isLocalhostRequest reports whether r was served from the developer's own machine.
func isLocalhostRequest(r *http.Request) bool {
	if slices.Contains(localhostIPs, clientIP(r)) {
		return true
	}

	host := r.Host
	if host == "" && r.URL != nil {
		host = r.URL.Host
	}
	// Relay matches subdomains for the request URL, which the SDK builds from r.Host.
	hostname := (&url.URL{Host: host}).Hostname()
	for _, domain := range localhostDomains {
		if hostname == domain || strings.HasSuffix(hostname, "."+domain) {
			return true
		}
	}

	// Relay compares the forwarded host exactly, without its port.
	forwardedHost, _, _ := strings.Cut(r.Header.Get("X-Forwarded-Host"), ":")
	return slices.Contains(localhostDomains, forwardedHost)
}

// clientIP returns the address of the client that sent r. Forwarding headers
// win over the socket address, because a reverse proxy on the same host
// connects over loopback.
func clientIP(r *http.Request) string {
	if forwardedFor := r.Header.Get("X-Forwarded-For"); forwardedFor != "" {
		first, _, _ := strings.Cut(forwardedFor, ",")
		return strings.Trim(strings.TrimSpace(first), "[]")
	}
	if realIP := r.Header.Get("X-Real-Ip"); realIP != "" {
		return strings.Trim(strings.TrimSpace(realIP), "[]")
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
