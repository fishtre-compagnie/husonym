package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/fishtre-compagnie/husonym/controlplane/console"
	"github.com/fishtre-compagnie/husonym/controlplane/cpstore"
	"github.com/stretchr/testify/require"
)

func Test_LoopbackAddr(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:8080", "127.8.9.10:0", "[::1]:8080", "localhost:8080"} {
		require.True(t, loopbackAddr(addr), addr)
	}
	for _, addr := range []string{":8080", "0.0.0.0:8080", "[::]:8080", "10.0.0.1:8080", "localhost.example.com:8080", "localhost"} {
		require.False(t, loopbackAddr(addr), addr)
	}
}

func Test_LoopbackBound(t *testing.T) {
	for _, addr := range []net.Addr{
		&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080},
		&net.TCPAddr{IP: net.IPv4(127, 8, 9, 10), Port: 8080},
		&net.TCPAddr{IP: net.IPv6loopback, Port: 8080},
	} {
		require.True(t, loopbackBound(addr), addr.String())
	}
	for _, addr := range []net.Addr{
		&net.TCPAddr{IP: net.IPv4zero, Port: 8080},
		&net.TCPAddr{IP: net.IPv6unspecified, Port: 8080},
		&net.TCPAddr{IP: net.IPv4(192, 0, 2, 10), Port: 8080},
		&net.TCPAddr{Port: 8080},
		&net.UnixAddr{Name: "/run/console.sock", Net: "unix"},
		&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8080},
	} {
		require.False(t, loopbackBound(addr), addr.String())
	}
	require.False(t, loopbackBound(nil))
}

// countingStore is a store whose first page is empty, and that counts how often it is asked.
type countingStore struct {
	console.Reader
	calls int
}

func (s *countingStore) Attention(context.Context, time.Time) (*cpstore.Attention, error) {
	s.calls++
	return &cpstore.Attention{}, nil
}

// A page open in the browser of the operator can make its own name resolve to this machine: the
// console then has to tell that it was not asked for under a name of this machine.
func Test_Backoffice_WithoutGates_AnswersOnlyLocalNames(t *testing.T) {
	store := &countingStore{}
	handler, err := newBackofficeHandler(readOnly(store, slog.New(slog.NewTextHandler(io.Discard, nil))), nil)
	require.NoError(t, err)

	for _, host := range []string{
		"attacker.example", "attacker.example:8080", "localhost.attacker.example:8080", "192.0.2.10:8080",
		"0.0.0.0:8080", "[::]:8080", "127.0.0.1.attacker.example", "",
	} {
		got := request(handler, http.MethodGet, host, "/", "")

		require.Equal(t, http.StatusNotFound, got.Code, host)
		require.Empty(t, got.Body.String(), host)
	}
	require.Zero(t, store.calls, "the store is not asked")

	local := []string{"127.0.0.1:8080", "[::1]:8080", "localhost:8080", "127.0.0.1", "[::1]", "localhost", "127.8.9.10:8080", "LocalHost:8080"}
	for _, host := range local {
		got := request(handler, http.MethodGet, host, "/", "")

		require.Equal(t, http.StatusOK, got.Code, host)
		require.Contains(t, got.Body.String(), "<h1>Needs attention</h1>", host)
	}
	require.Equal(t, len(local), store.calls)

	require.Equal(t, http.StatusOK, request(handler, http.MethodGet, "attacker.example", "/healthz", "").Code,
		"the health check stays outside")
}
