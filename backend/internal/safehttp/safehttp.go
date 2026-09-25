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
	"net/netip"
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
	// TrustedOrigins are the origins -- scheme, host and port -- of the deployment's own
	// provider, which the operator configured: reached whatever their address, through
	// the environment's proxy if any. An account's provider never is one of them.
	TrustedOrigins []string
}

// NewPolicy returns a policy trusting the origins of the given URLs; an empty or malformed
// one trusts nothing.
func NewPolicy(allowPrivate bool, trustedURLs ...string) Policy {
	policy := Policy{AllowPrivate: allowPrivate}
	for _, raw := range trustedURLs {
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" {
			continue
		}
		policy.TrustedOrigins = append(policy.TrustedOrigins, origin(u))
	}
	return policy
}

// origin is how two URLs are told to be the same server: scheme, host and port, with the
// scheme's port made explicit.
func origin(u *url.URL) string {
	port := u.Port()
	if port == "" {
		switch strings.ToLower(u.Scheme) {
		case "https":
			port = "443"
		case "http":
			port = "80"
		}
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Hostname()) + ":" + port
}

func (p Policy) trusts(u *url.URL) bool {
	return slices.Contains(p.TrustedOrigins, origin(u))
}

// CheckURL refuses a URL the policy does not let a request go to on its scheme alone:
// https, or http where the policy allows it or the origin is trusted; nothing else, ever.
func (p Policy) CheckURL(u *url.URL) error {
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" && (p.AllowPrivate || p.trusts(u)) {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrNotHTTPS, u.Scheme)
}

// CheckIssuer tells whether an account may declare an issuer: an https URL -- no query,
// fragment or credentials, which would steer the requests made from it -- whose host
// resolves to public addresses only. An account's issuer is never trusted, not even on
// the deployment's own provider: that takes the operator's AUTH_ACCOUNT_ISSUER_ALLOW_PRIVATE.
// It is what saving a setting checks; fetching from the issuer later checks again, at
// connection time, through the Transport.
func (p Policy) CheckIssuer(ctx context.Context, issuer string) error {
	u, err := url.Parse(issuer)
	if err != nil {
		return fmt.Errorf("the issuer is not a URL: %q", issuer)
	}
	if err := p.CheckIssuerURL(u); err != nil {
		return err
	}
	if p.AllowPrivate {
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

// CheckIssuerURL is what CheckIssuer asks of the URL itself, before resolving its host.
func (p Policy) CheckIssuerURL(u *url.URL) error {
	if u.Hostname() == "" || u.Opaque != "" {
		return fmt.Errorf("the issuer is not a URL: %q", u.String())
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil || u.ForceQuery {
		return fmt.Errorf("the issuer is a URL without query, fragment or credentials: %q", u.Redacted())
	}
	if u.Scheme != "https" && (u.Scheme != "http" || !p.AllowPrivate) {
		return fmt.Errorf("%w: %s", ErrNotHTTPS, u.Scheme)
	}
	return nil
}

// Transport returns a round tripper that applies the policy to every request, redirects
// included: each hop is a request of its own. timeout bounds dialing, the TLS handshake and
// the wait for response headers; without keepAlive, each request has its own connection.
func (p Policy) Transport(timeout time.Duration, keepAlive bool) http.RoundTripper {
	newTransport := func(
		proxy func(*http.Request) (*url.URL, error),
		control func(network, address string, _ syscall.RawConn) error,
	) *http.Transport {
		dialer := &net.Dialer{Timeout: timeout, Control: control}
		return &http.Transport{
			Proxy:                 proxy,
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   timeout,
			ResponseHeaderTimeout: timeout,
			IdleConnTimeout:       90 * time.Second,
			DisableKeepAlives:     !keepAlive,
			ForceAttemptHTTP2:     true,
		}
	}
	// What the operator reaches -- its own provider, or every provider when it allows
	// private ones -- goes through the environment's proxy as before. What an account
	// chose does not: a proxy would be a way around the dialer, since the connection would
	// then be made to the proxy and the destination carried in the request.
	open := newTransport(http.ProxyFromEnvironment, nil)
	checked := open
	if !p.AllowPrivate {
		checked = newTransport(nil, func(network, address string, _ syscall.RawConn) error {
			return checkDialAddress(network, address)
		})
	}
	return &transport{policy: p, open: open, checked: checked}
}

type transport struct {
	policy  Policy
	open    *http.Transport
	checked *http.Transport
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.policy.trusts(req.URL) && sameOriginChain(req) {
		return t.open.RoundTrip(req)
	}
	if err := t.policy.CheckURL(req.URL); err != nil {
		return nil, err
	}
	return t.checked.RoundTrip(req)
}

// sameOriginChain tells whether every request that redirected to this one was for its
// origin: a trusted origin is trusted when it is where the request started, not when
// something else redirected there.
func sameOriginChain(req *http.Request) bool {
	want := origin(req.URL)
	for res := req.Response; res != nil && res.Request != nil; res = res.Request.Response {
		if origin(res.Request.URL) != want {
			return false
		}
	}
	return true
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
// It is a deny-by-default list: anything that is not plainly public is refused. IPv4
// written in one of IPv6's forms -- mapped, translated, NAT64, 6to4 -- is unwrapped first,
// or ::ffff:127.0.0.1 would walk straight past a check written for IPv4. The web server
// holds the same table (public-address.ts); a change here is a change there.
func IsPublicAddress(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if embedded, ok := embeddedIPv4(addr); ok {
		addr = embedded
	}
	for _, prefix := range blockedPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

// embeddedIPv4 is the IPv4 address an IPv6 one carries: SIIT (::ffff:0:a.b.c.d), NAT64
// (64:ff9b::/96) and 6to4 (2002::/16).
func embeddedIPv4(addr netip.Addr) (netip.Addr, bool) {
	if !addr.Is6() {
		return addr, false
	}
	b := addr.As16()
	switch {
	case siitPrefix.Contains(addr), nat64Prefix.Contains(addr):
		return netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}), true
	case sixToFourPrefix.Contains(addr):
		return netip.AddrFrom4([4]byte{b[2], b[3], b[4], b[5]}), true
	}
	return addr, false
}

var (
	siitPrefix      = netip.MustParsePrefix("::ffff:0:0:0/96")
	nat64Prefix     = netip.MustParsePrefix("64:ff9b::/96")
	sixToFourPrefix = netip.MustParsePrefix("2002::/16")

	// blockedPrefixes are the ranges that are not the public internet: this host, its
	// networks, the cloud's metadata service, and what no one routes to.
	blockedPrefixes = []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"),      // "this network"
		netip.MustParsePrefix("10.0.0.0/8"),     // private
		netip.MustParsePrefix("100.64.0.0/10"),  // carrier-grade NAT, where cluster addressing ends up
		netip.MustParsePrefix("127.0.0.0/8"),    // loopback
		netip.MustParsePrefix("169.254.0.0/16"), // link-local -- cloud metadata lives here
		netip.MustParsePrefix("172.16.0.0/12"),  // private
		netip.MustParsePrefix("192.0.0.0/24"),   // IETF protocol assignments
		netip.MustParsePrefix("192.168.0.0/16"), // private
		netip.MustParsePrefix("198.18.0.0/15"),  // benchmarking
		netip.MustParsePrefix("224.0.0.0/4"),    // multicast
		netip.MustParsePrefix("240.0.0.0/4"),    // reserved, broadcast
		netip.MustParsePrefix("::/96"),          // unspecified, loopback, IPv4-compatible
		netip.MustParsePrefix("64:ff9b:1::/48"), // local-use NAT64
		netip.MustParsePrefix("fc00::/7"),       // unique local
		netip.MustParsePrefix("fe80::/10"),      // link-local
		netip.MustParsePrefix("fec0::/10"),      // site-local
		netip.MustParsePrefix("ff00::/8"),       // multicast
	}
)
