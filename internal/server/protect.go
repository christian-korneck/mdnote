package server

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
)

// Protect wraps the HTTP handler of the mdnote server (not the desktop app, which
// opens no port) against attacks through the user's browser:
//
//   - DNS rebinding: the Host header must name this server: an IP address,
//     localhost, this machine's hostname, or one of extraHosts. A rebinding page
//     always uses the attacker's own domain, so it is rejected.
//   - Cross-site requests: state-changing requests (POST, PUT, PATCH, DELETE) that
//     a browser marks as cross-site (Sec-Fetch-Site, or an Origin that doesn't match
//     Host) are rejected, using net/http's CrossOriginProtection. Requests without
//     these headers, such as curl, are allowed.
//
// There is still no authentication: anyone who can connect can use the API.
func Protect(h http.Handler, extraHosts []string) http.Handler {
	names := map[string]bool{}
	if hn, err := os.Hostname(); err == nil && hn != "" {
		hn = normalizeHost(hn)
		names[hn] = true
		names[hn+".local"] = true
	}
	for _, x := range extraHosts {
		if x = normalizeHost(x); x != "" {
			names[x] = true
		}
	}
	cop := http.NewCrossOriginProtection()
	inner := cop.Handler(h)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hostAllowed(r.Host, names) {
			http.Error(w, fmt.Sprintf("host %q not allowed; add it with -allow-host", r.Host), http.StatusForbidden)
			return
		}
		inner.ServeHTTP(w, r)
	})
}

// hostAllowed reports whether the host part of a Host header is an IP address,
// localhost (or *.localhost), or one of names.
func hostAllowed(hostport string, names map[string]bool) bool {
	host := normalizeHost(hostport)
	if host == "" {
		return false
	}
	if net.ParseIP(host) != nil {
		return true
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	return names[host]
}

// normalizeHost strips the port and IPv6 brackets, lowercases, and drops a
// trailing dot ("Example.COM.:9090" -> "example.com").
func normalizeHost(hostport string) string {
	host := strings.TrimSpace(hostport)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	return strings.TrimSuffix(strings.ToLower(host), ".")
}
