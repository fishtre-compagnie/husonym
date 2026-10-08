// Package accessgate holds the two gates every request to the backoffice passes: the host it was
// sent to, then the Cloudflare Access token it carries.
package accessgate

import (
	"net"
	"net/http"
	"strings"
)

// Host lets a request through only when it was sent to host: the Host of the request, its port
// set aside, compared without regard to case. Any other request, and one that names no host, is
// answered 404 with no body and no log line, as if nothing were served here. host is a bare name,
// without a port.
//
// It comes before the Access gate: under another name the console does not exist, whatever the
// request carries.
func Host(host string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := hostname(r.Host)
		if name == "" || !strings.EqualFold(name, host) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// hostname strips the port of a Host header, when it has one.
func hostname(hostport string) string {
	if name, _, err := net.SplitHostPort(hostport); err == nil {
		return name
	}
	return hostport
}
