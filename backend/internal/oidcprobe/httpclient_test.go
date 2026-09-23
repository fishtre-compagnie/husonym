package oidcprobe

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// Trying a setting makes this deployment fetch a URL somebody typed into a form. These
// are the bounds on that, and each case here is a way the request would otherwise reach
// something that is not on the internet.
func Test_isPublicAddress(t *testing.T) {
	blocked := []struct {
		name string
		ip   string
	}{
		{"loopback v4", "127.0.0.1"},
		{"loopback, another one in the range", "127.42.7.9"},
		{"loopback v6", "::1"},
		{"unspecified v4", "0.0.0.0"},
		{"unspecified v6", "::"},
		{"private 10/8", "10.0.0.1"},
		{"private 172.16/12", "172.20.13.4"},
		{"private 192.168/16", "192.168.1.1"},
		{"unique local v6", "fd00::1"},
		// The one that matters most on a cloud host: the instance metadata service, which
		// hands out credentials to anything that can make it a request.
		{"link-local, where cloud metadata lives", "169.254.169.254"},
		{"link-local v6", "fe80::1"},
		{"multicast", "224.0.0.1"},
		{"carrier-grade NAT", "100.64.0.1"},
		{"carrier-grade NAT, top of range", "100.127.255.255"},
		// Written as IPv6, these are the same destinations. A check that only understood
		// IPv4 would wave them through.
		{"IPv4-mapped loopback", "::ffff:127.0.0.1"},
		{"IPv4-mapped metadata", "::ffff:169.254.169.254"},
		{"6to4 wrapping a private address", "2002:0a00:0001::"},
		{"NAT64 wrapping metadata", "64:ff9b::a9fe:a9fe"},
	}
	for _, tc := range blocked {
		t.Run("blocked: "+tc.name, func(t *testing.T) {
			ip := net.ParseIP(tc.ip)
			require.NotNil(t, ip, "the test address itself must parse")
			require.False(t, isPublicAddress(ip))
		})
	}

	allowed := []struct {
		name string
		ip   string
	}{
		{"a public v4", "93.184.216.34"},
		{"a public v6", "2606:2800:220:1:248:1893:25c8:1946"},
		{"just outside carrier-grade NAT", "100.128.0.1"},
		{"just below carrier-grade NAT", "100.63.255.255"},
	}
	for _, tc := range allowed {
		t.Run("allowed: "+tc.name, func(t *testing.T) {
			ip := net.ParseIP(tc.ip)
			require.NotNil(t, ip)
			require.True(t, isPublicAddress(ip))
		})
	}
}

func Test_checkDialAddress(t *testing.T) {
	t.Run("refuses a network that is not tcp", func(t *testing.T) {
		require.ErrorIs(t, checkDialAddress("udp", "93.184.216.34:443"), ErrBlockedAddress)
	})
	t.Run("refuses an address it cannot read", func(t *testing.T) {
		require.ErrorIs(t, checkDialAddress("tcp", "not-an-address"), ErrBlockedAddress)
	})
	t.Run("accepts a public address", func(t *testing.T) {
		require.NoError(t, checkDialAddress("tcp", "93.184.216.34:443"))
	})
}

// The end-to-end version of the above: a real server on loopback, which is what a hostile
// issuer pointing at the deployment itself would look like.
func Test_get_refusesLoopback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Over https, to get past the scheme check and reach the dialer.
	target := "https://" + server.Listener.Addr().String()
	_, err := get(context.Background(), newSafeClient(), target)
	require.Error(t, err)
	require.Contains(t, err.Error(), ErrBlockedAddress.Error())
}

func Test_get_refusesPlainHTTP(t *testing.T) {
	_, err := get(context.Background(), newSafeClient(), "http://example.com/.well-known/openid-configuration")
	require.ErrorIs(t, err, ErrNotHTTPS)
}

func Test_get_refusesSomethingThatIsNotAURL(t *testing.T) {
	_, err := get(context.Background(), newSafeClient(), "://nope")
	require.Error(t, err)
}
