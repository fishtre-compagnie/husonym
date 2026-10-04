package webhook

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"syscall"

	"github.com/fishtre-compagnie/husonym/internal/safehttp"
)

// errDestinationRefused is the cause of a connection that checkDestination did not allow.
var errDestinationRefused = errors.New("webhooks are not sent to this address")

// awsIPv6Metadata is where AWS serves the instance metadata over IPv6. It is a unique
// local address, a range that is otherwise reachable.
var awsIPv6Metadata = netip.MustParseAddr("fd00:ec2::254")

// checkDestination is the Control function of the dialer: it sees the address a connection
// is about to be made to, once the name of the URL is resolved, for every connection. A
// check of the URL would not: a name may resolve to anything, and differently each time.
//
// Receivers on the deployment's own network are the usual case, so loopback and private
// addresses are reachable. Link-local addresses are not: the metadata service of a cloud
// host lives there and answers anything that reaches it. Nor are the unspecified address
// and multicast, which are no receiver.
func checkDestination(network, address string, _ syscall.RawConn) error {
	if network != "tcp4" && network != "tcp6" {
		return fmt.Errorf("%w: network %s", errDestinationRefused, network)
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%w: %s", errDestinationRefused, address)
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("%w: %s", errDestinationRefused, host)
	}
	addr = safehttp.UnwrapIPv4(addr.WithZone(""))
	if addr.IsLinkLocalUnicast() || addr.IsUnspecified() || addr.IsMulticast() || addr == awsIPv6Metadata {
		return fmt.Errorf("%w: %s", errDestinationRefused, addr)
	}
	return nil
}
