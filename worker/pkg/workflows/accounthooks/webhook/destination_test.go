package webhook

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_CheckDestination(t *testing.T) {
	tests := []struct {
		name    string
		network string
		address string
		refused bool
	}{
		{"public ipv4", "tcp4", "93.184.216.34:443", false},
		{"public ipv6", "tcp6", "[2606:2800:220:1:248:1893:25c8:1946]:443", false},
		{"loopback ipv4", "tcp4", "127.0.0.1:8080", false},
		{"loopback ipv6", "tcp6", "[::1]:8080", false},
		{"private 10/8", "tcp4", "10.1.2.3:80", false},
		{"private 172.16/12", "tcp4", "172.16.0.1:80", false},
		{"private 192.168/16", "tcp4", "192.168.1.1:80", false},
		{"carrier-grade nat", "tcp4", "100.64.0.1:80", false},
		{"unique local", "tcp6", "[fd12:3456:789a::1]:80", false},

		{"link-local ipv4, the metadata address", "tcp4", "169.254.169.254:80", true},
		{"link-local ipv4, any", "tcp4", "169.254.0.1:80", true},
		{"link-local ipv6", "tcp6", "[fe80::1]:80", true},
		{"link-local ipv6 with a zone", "tcp6", "[fe80::1%eth0]:80", true},
		{"the ipv6 metadata address", "tcp6", "[fd00:ec2::254]:80", true},
		{"unspecified ipv4", "tcp4", "0.0.0.0:80", true},
		{"unspecified ipv6", "tcp6", "[::]:80", true},
		{"multicast ipv4", "tcp4", "224.0.0.1:80", true},
		{"multicast ipv6", "tcp6", "[ff02::1]:80", true},
		{"link-local ipv4 mapped in ipv6", "tcp6", "[::ffff:169.254.169.254]:80", true},
		{"link-local ipv4 behind nat64", "tcp6", "[64:ff9b::a9fe:a9fe]:80", true},
		{"link-local ipv4 behind 6to4", "tcp6", "[2002:a9fe:a9fe::1]:80", true},

		// Metadata services that live outside the link-local ranges.
		{"a metadata address in the carrier-grade nat range", "tcp4", "100.100.100.200:80", true},
		{"a metadata address in the protocol assignments range", "tcp4", "192.0.0.192:80", true},
		{"a metadata address in the unique local range", "tcp6", "[fd20:ce::254]:80", true},
		{"its neighbor in the carrier-grade nat range", "tcp4", "100.100.100.201:80", false},
		{"its neighbor in the unique local range", "tcp6", "[fd20:ce::253]:80", false},

		// A refused IPv4 address is refused in the IPv6 forms that are known to carry it.
		{"link-local ipv4 in the ipv4-compatible form", "tcp6", "[::a9fe:a9fe]:80", true},
		{"a metadata address in the ipv4-compatible form", "tcp6", "[::6464:64c8]:80", true},
		{"a metadata address mapped in ipv6", "tcp6", "[::ffff:100.100.100.200]:80", true},
		{"a metadata address behind nat64", "tcp6", "[64:ff9b::6464:64c8]:80", true},
		{"a metadata address behind 6to4", "tcp6", "[2002:6464:64c8::1]:80", true},
		{"private ipv4 in the ipv4-compatible form", "tcp6", "[::a00:1]:80", false},

		// Where the local-use NAT64 prefix holds an IPv4 address depends on how a network
		// cut it, which the address does not tell: nothing is read into it.
		{"local-use nat64, 10.0.0.1 after a /48", "tcp6", "[64:ff9b:1:a00:0:100::]:80", false},
		{"local-use nat64, 8.8.8.8 after a /48", "tcp6", "[64:ff9b:1:808:8:800::]:80", false},
		{"local-use nat64, 10.0.0.1 after a /96 with a subnet", "tcp6", "[64:ff9b:1:e123::a00:1]:80", false},
		{"local-use nat64, 10.0.0.1 after a /96", "tcp6", "[64:ff9b:1::a00:1]:80", false},
		{"local-use nat64, any address", "tcp6", "[64:ff9b:1::a9fe:a9fe]:80", false},

		{"a network that is not tcp", "udp4", "93.184.216.34:443", true},
		{"an address without a port", "tcp4", "93.184.216.34", true},
		{"a name in place of an address", "tcp4", "example.com:443", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkDestination(tt.network, tt.address, nil)
			if tt.refused {
				require.ErrorIs(t, err, errDestinationRefused)
				return
			}
			require.NoError(t, err)
		})
	}
}
