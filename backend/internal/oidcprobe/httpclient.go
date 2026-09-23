package oidcprobe

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"time"
)

// Trying a setting means fetching a URL somebody typed into a form. That is a request the
// server makes on behalf of an unprivileged caller, to a destination the caller chooses --
// a server-side request forgery unless it is bounded. Everything in this file is that
// bound.
const (
	// requestTimeout caps a single request. A provider that cannot answer a static
	// document in this time is a provider whose users would not get in either.
	requestTimeout = 5 * time.Second
	// totalTimeout caps the whole probe, redirects included, so a chain of slow hops
	// cannot hold a handler open.
	totalTimeout = 15 * time.Second
	// maxBodyBytes caps what is read. A discovery document is a few kilobytes; anything
	// larger is either not one or is trying to be a problem.
	maxBodyBytes = 1 << 20 // 1 MiB
	// maxRedirects is what a correctly configured provider needs, plus room for the
	// http-to-https and trailing-slash hops that real deployments have.
	maxRedirects = 5
)

var (
	// ErrBlockedAddress is returned when a hostname resolves into an address range that
	// belongs to the deployment rather than to the internet.
	ErrBlockedAddress = errors.New("the address is not on the public internet")
	// ErrNotHTTPS is returned for any URL that is not https.
	ErrNotHTTPS = errors.New("only https is accepted")
)

// newSafeClient returns a client that will only ever talk to a public https endpoint.
//
// The address check lives in the dialer rather than in a check of the URL, and that is the
// whole point: a hostname is resolved at connection time, so checking the name first and
// connecting after leaves the gap where the name resolves to something else in between --
// and the check would have to be repeated on every redirect. In the dialer it holds for
// the first hop, every redirect, and every address a name resolves to.
func newSafeClient() *http.Client {
	dialer := &net.Dialer{
		Timeout: requestTimeout,
		Control: func(network, address string, _ syscall.RawConn) error {
			return checkDialAddress(network, address)
		},
	}

	return &http.Client{
		Timeout: totalTimeout,
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   requestTimeout,
			ResponseHeaderTimeout: requestTimeout,
			DisableKeepAlives:     true,
			// A proxy from the environment would be a way around the dialer, since the
			// connection would then be made to the proxy and the destination carried in
			// the request.
			Proxy: nil,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			return requireHTTPS(req.URL)
		},
	}
}

func checkDialAddress(network, address string) error {
	switch network {
	case "tcp", "tcp4", "tcp6":
	default:
		return fmt.Errorf("%w: unexpected network %q", ErrBlockedAddress, network)
	}

	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrBlockedAddress, address)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// The dialer has already resolved the name by now, so anything that is not an
		// address here is something unexpected rather than a hostname.
		return fmt.Errorf("%w: %s", ErrBlockedAddress, host)
	}
	if !isPublicAddress(ip) {
		return fmt.Errorf("%w: %s", ErrBlockedAddress, ip)
	}
	return nil
}

// isPublicAddress reports whether an address is one the internet can route to.
//
// It is a deny-by-default list: anything that is not plainly public is refused. The
// IPv4-in-IPv6 forms are unwrapped first, or ::ffff:127.0.0.1 would walk straight past a
// check written for IPv4.
func isPublicAddress(ip net.IP) bool {
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	switch {
	case ip.IsUnspecified(), // 0.0.0.0, ::
		ip.IsLoopback(),         // 127.0.0.0/8, ::1
		ip.IsPrivate(),          // 10/8, 172.16/12, 192.168/16, fc00::/7
		ip.IsLinkLocalUnicast(), // 169.254/16 -- cloud metadata lives here
		ip.IsLinkLocalMulticast(),
		ip.IsInterfaceLocalMulticast(),
		ip.IsMulticast():
		return false
	}
	// 100.64.0.0/10, carrier-grade NAT, which is where a good deal of container and
	// cluster addressing ends up.
	if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
		return false
	}
	// IPv4-mapped and 6to4 forms of the above would otherwise slip through as opaque v6.
	if ip.To4() == nil && len(ip) == net.IPv6len {
		// 2002::/16 carries an embedded IPv4 address.
		if ip[0] == 0x20 && ip[1] == 0x02 {
			return isPublicAddress(net.IPv4(ip[2], ip[3], ip[4], ip[5]))
		}
		// 64:ff9b::/96, NAT64.
		if ip[0] == 0x00 && ip[1] == 0x64 && ip[2] == 0xff && ip[3] == 0x9b {
			return isPublicAddress(net.IPv4(ip[12], ip[13], ip[14], ip[15]))
		}
	}
	return true
}

func requireHTTPS(u *url.URL) error {
	if u.Scheme != "https" {
		return fmt.Errorf("%w: %s", ErrNotHTTPS, u.Scheme)
	}
	return nil
}

// get fetches a URL under every bound in this file.
func get(ctx context.Context, client *http.Client, rawURL string) (*http.Response, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("not a URL: %w", err)
	}
	if err := requireHTTPS(parsed); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	return client.Do(req)
}
