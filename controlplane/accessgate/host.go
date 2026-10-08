// Package accessgate holds the two gates every request to the backoffice passes: the host it was
// sent to, then the Cloudflare Access token it carries.
package accessgate

import (
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// Host lets a request through only when it was sent to host: the Host of the request, its port
// set aside, compared without regard to case. Any other request, and one that names no host, is
// answered 404 with no body and no log line, as if nothing were served here. host is a bare name,
// without a port: see ValidHost. Only the Host of the request counts, never a header that
// claims to forward one.
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

// ValidHost says whether host can be given to Host: a bare host name, in any case, with no
// scheme, no port, no path and no trailing dot. Host cannot fail, and with another value it
// would let nothing through: the command checks first, so that a wrong setting stops the start
// and does not show as a console that answers 404. The value is not echoed in the error.
func ValidHost(host string) error {
	if !bareHost(host) {
		return errors.New("the host of the backoffice must be a bare host name: no scheme, no port, no path, no trailing dot")
	}
	return nil
}

// bareHost says whether s is a host name and nothing more.
func bareHost(s string) bool {
	if s == "" || strings.HasSuffix(s, ".") || strings.ContainsAny(s, " \t\r\n:/\\?#@[]") {
		return false
	}
	parsed, err := url.Parse("https://" + s)
	return err == nil && parsed.Host == s && parsed.Port() == ""
}

// hostname strips the port of a Host header, when it has one.
func hostname(hostport string) string {
	if name, _, err := net.SplitHostPort(hostport); err == nil {
		return name
	}
	return hostport
}
