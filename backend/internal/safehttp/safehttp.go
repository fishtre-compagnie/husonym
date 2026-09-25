// Package safehttp bounds where this deployment may be made to send a request.
//
// An account declares its own identity provider, and this deployment then fetches what that
// provider publishes: its discovery document, its keys. The address is chosen by whoever
// administers the account, not by the operator -- a server-side request forgery unless it
// is bounded. The bound is a policy: https, to an address the internet routes to, checked
// when the connection is made -- for the first hop, every redirect, and every address a
// name resolves to -- with the deployment's own provider let through, since it commonly
// lives on the deployment's network.
package safehttp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"syscall"
	"time"
)

var (
	// ErrBlockedAddress is returned when a host resolves into an address range that belongs
	// to the deployment rather than to the internet.
	ErrBlockedAddress = errors.New("the address is not on the public internet")
	// ErrNotHTTPS is returned for any URL that is not https.
	ErrNotHTTPS = errors.New("only https is accepted")
)

// Policy says where a client may send requests.
type Policy struct {
	// AllowPrivate lets through plain http and any address: a deployment whose providers
	// live on its own network, or a development environment. Set by the operator
	// (AUTH_ACCOUNT_ISSUER_ALLOW_PRIVATE), never by an account.
	AllowPrivate bool
	// TrustedHosts are reached whatever their scheme and address: the hosts of the
	// deployment's own provider, which the operator configured.
	TrustedHosts []string
}

// NewPolicy returns a policy trusting the hosts of the given URLs; an empty or malformed
// one trusts nothing.
func NewPolicy(allowPrivate bool, trustedURLs ...string) Policy {
	policy := Policy{AllowPrivate: allowPrivate}
	for _, raw := range trustedURLs {
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" {
			continue
		}
		policy.TrustedHosts = append(policy.TrustedHosts, strings.ToLower(u.Hostname()))
	}
	return policy
}

func (p Policy) trusts(host string) bool {
	return slices.Contains(p.TrustedHosts, strings.ToLower(host))
}

// CheckURL refuses a URL the policy does not let a request go to on its scheme alone:
// https, or http where the policy allows it; nothing else, ever.
func (p Policy) CheckURL(u *url.URL) error {
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" && (p.AllowPrivate || p.trusts(u.Hostname())) {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrNotHTTPS, u.Scheme)
}

// CheckIssuer tells whether an issuer may be declared: an https URL whose host resolves to
// public addresses only. It is what saving a setting checks; fetching from the issuer
// later checks again, at connection time, through the Transport.
func (p Policy) CheckIssuer(ctx context.Context, issuer string) error {
	u, err := url.Parse(issuer)
	if err != nil || u.Hostname() == "" {
		return fmt.Errorf("the issuer is not a URL: %q", issuer)
	}
	if err := p.CheckURL(u); err != nil {
		return err
	}
	if p.AllowPrivate || p.trusts(u.Hostname()) {
		return nil
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, u.Hostname())
	if err != nil {
		return fmt.Errorf("the issuer's host does not resolve: %w", err)
	}
	for _, addr := range addrs {
		if !IsPublicAddress(addr.IP) {
			return fmt.Errorf("%w: %s resolves to %s", ErrBlockedAddress, u.Hostname(), addr.IP)
		}
	}
	return nil
}

// Transport returns a round tripper that applies the policy to every request, redirects
// included: each hop is a request of its own. timeout bounds dialing, the TLS handshake
// and the wait for response headers.
func (p Policy) Transport(timeout time.Duration) http.RoundTripper {
	newTransport := func(control func(network, address string, _ syscall.RawConn) error) *http.Transport {
		dialer := &net.Dialer{Timeout: timeout, Control: control}
		return &http.Transport{
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   timeout,
			ResponseHeaderTimeout: timeout,
			// A proxy from the environment would be a way around the dialer, since the
			// connection would then be made to the proxy and the destination carried in
			// the request.
			Proxy: nil,
		}
	}
	var checked *http.Transport
	if p.AllowPrivate {
		checked = newTransport(nil)
	} else {
		checked = newTransport(func(network, address string, _ syscall.RawConn) error {
			return checkDialAddress(network, address)
		})
	}
	return &transport{policy: p, trusted: newTransport(nil), checked: checked}
}

type transport struct {
	policy  Policy
	trusted *http.Transport
	checked *http.Transport
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.policy.trusts(req.URL.Hostname()) {
		return t.trusted.RoundTrip(req)
	}
	if err := t.policy.CheckURL(req.URL); err != nil {
		return nil, err
	}
	return t.checked.RoundTrip(req)
}

// The address check lives in the dialer rather than in a check of the URL, and that is
// the whole point: a host name is resolved at connection time, so checking the name first
// and connecting after leaves the gap where the name resolves to something else in
// between. In the dialer it holds for every hop and every address a name resolves to.
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
		// address here is something unexpected rather than a host name.
		return fmt.Errorf("%w: %s", ErrBlockedAddress, host)
	}
	if !IsPublicAddress(ip) {
		return fmt.Errorf("%w: %s", ErrBlockedAddress, ip)
	}
	return nil
}

// IsPublicAddress reports whether an address is one the internet can route to.
//
// It is a deny-by-default list: anything that is not plainly public is refused. The
// IPv4-in-IPv6 forms are unwrapped first, or ::ffff:127.0.0.1 would walk straight past a
// check written for IPv4.
func IsPublicAddress(ip net.IP) bool {
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
			return IsPublicAddress(net.IPv4(ip[2], ip[3], ip[4], ip[5]))
		}
		// 64:ff9b::/96, NAT64.
		if ip[0] == 0x00 && ip[1] == 0x64 && ip[2] == 0xff && ip[3] == 0x9b {
			return IsPublicAddress(net.IPv4(ip[12], ip[13], ip[14], ip[15]))
		}
	}
	return true
}
