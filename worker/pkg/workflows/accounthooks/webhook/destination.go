package webhook

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"syscall"

	"github.com/fishtre-compagnie/husonym/internal/safehttp"
)

// errDestinationRefused is the cause of a connection that checkDestination did not allow.
var errDestinationRefused = errors.New("webhooks are not sent to this address")

// metadataAddresses are where cloud hosts serve their instance metadata outside the
// link-local ranges. They sit in ranges that are otherwise reachable.
var metadataAddresses = []netip.Addr{
	netip.MustParseAddr("100.100.100.200"),
	netip.MustParseAddr("192.0.0.192"),
	netip.MustParseAddr("fd00:ec2::254"),
	netip.MustParseAddr("fd20:ce::254"),
}

var ipv4CompatiblePrefix = netip.MustParsePrefix("::/96")

// checkDestination is the Control function of the dialer: it sees the address a connection
// is about to be made to, once the name of the URL is resolved, for every connection. A
// check of the URL would not: a name may resolve to anything, and differently each time.
//
// Receivers on the deployment's own network are the usual case, so loopback and private
// addresses are reachable. Link-local addresses are not: the metadata service of a cloud
// host lives there and answers anything that reaches it. Nor are the metadata addresses
// outside that range, the unspecified address and multicast, which are no receiver.
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
	addr = addr.WithZone("")
	if slices.ContainsFunc(carried(addr), refused) {
		return fmt.Errorf("%w: %s", errDestinationRefused, addr)
	}
	return nil
}

func refused(addr netip.Addr) bool {
	return addr.IsLinkLocalUnicast() || addr.IsUnspecified() || addr.IsMulticast() ||
		slices.Contains(metadataAddresses, addr)
}

// carried returns the address and the IPv4 address it carries when it is written in an
// IPv6 form that says where the IPv4 address sits: mapped, translated, under the NAT64
// prefix 64:ff9b::/96, 6to4, and IPv4-compatible. A refused IPv4 address is refused in
// those forms too. Other prefixes hold an IPv4 address at a place that depends on how a
// network cut them, which an address alone does not tell: nothing is read into them.
func carried(addr netip.Addr) []netip.Addr {
	addrs := []netip.Addr{addr, safehttp.UnwrapIPv4(addr)}
	if addr.Is6() && !addr.Is4In6() && ipv4CompatiblePrefix.Contains(addr) {
		b := addr.As16()
		addrs = append(addrs, netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}))
	}
	return addrs
}
