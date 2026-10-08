package main

import (
	"net"
	"net/http"
	"strings"
)

// What keeps the console to this machine when it is served without its gates: where it listens,
// and the name it is asked for under.

// loopbackAddr says whether a listen address names this machine only: localhost, or an address
// of 127.0.0.0/8 or ::1. An address without a host listens on every interface.
func loopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	return err == nil && loopbackName(host)
}

// loopbackName says whether a host name, without a port, is one of this machine only: localhost,
// or an address of 127.0.0.0/8 or ::1 written as an address.
func loopbackName(name string) bool {
	if strings.EqualFold(name, "localhost") {
		return true
	}
	ip := net.ParseIP(name)
	return ip != nil && ip.IsLoopback()
}

// loopbackBound says whether the address a listener is bound to is a loopback one. The name of
// the listen address was judged before; this judges what it resolved to.
func loopbackBound(addr net.Addr) bool {
	bound, ok := addr.(*net.TCPAddr)
	return ok && bound.IP.IsLoopback()
}

// localNames lets a request through only when it was sent under a name of this machine: the Host
// of the request, its port set aside, is localhost or a loopback address. Any other request is
// answered 404 with no body and no log line.
//
// Listening on a loopback address does not keep the browser of the operator out: a page open in
// it can make its own name resolve to 127.0.0.1 and would then read the console as its own
// origin. That page cannot choose the Host its requests carry, which stays its own name.
func localNames(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !loopbackName(requestHostname(r.Host)) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requestHostname strips the port of a Host header, when it has one, and the brackets of an IPv6
// address written without a port.
func requestHostname(hostport string) string {
	if name, _, err := net.SplitHostPort(hostport); err == nil {
		return name
	}
	if strings.HasPrefix(hostport, "[") && strings.HasSuffix(hostport, "]") {
		return hostport[1 : len(hostport)-1]
	}
	return hostport
}
