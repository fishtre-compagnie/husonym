package accessgate_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fishtre-compagnie/husonym/controlplane/accessgate"
)

func Test_Host(t *testing.T) {
	const console = "console.example.com"
	cases := []struct {
		name       string
		configured string
		host       string
		forwarded  string
		status     int
	}{
		{"the host of the console", console, "console.example.com", "", http.StatusOK},
		{"with a port", console, "console.example.com:8443", "", http.StatusOK},
		{"in upper case", console, "CONSOLE.Example.COM", "", http.StatusOK},
		{"in upper case with a port", console, "CONSOLE.EXAMPLE.COM:443", "", http.StatusOK},
		{"configured in upper case", "Console.Example.com", "console.example.com", "", http.StatusOK},
		{"another host", console, "api.example.com", "", http.StatusNotFound},
		{"another host with a port", console, "api.example.com:8443", "", http.StatusNotFound},
		{"a host that only ends like it", console, "evil-console.example.com", "", http.StatusNotFound},
		{"a host that only starts like it", console, "console.example.com.evil.test", "", http.StatusNotFound},
		{"with a trailing dot", console, "console.example.com.", "", http.StatusNotFound},
		{"with a trailing dot and a port", console, "console.example.com.:8443", "", http.StatusNotFound},
		{"an IPv6 literal with a port", console, "[::1]:8443", "", http.StatusNotFound},
		{"an IPv6 literal", console, "[::1]", "", http.StatusNotFound},
		{"an address", console, "192.0.2.10:8443", "", http.StatusNotFound},
		{"an empty host", console, "", "", http.StatusNotFound},
		{"only a port", console, ":8443", "", http.StatusNotFound},
		{"an empty host when none is configured", "", "", "", http.StatusNotFound},
		// Only the Host counts: a header anybody can add says nothing of where a request was sent.
		{"another host, forwarded as the console", console, "api.example.com", console, http.StatusNotFound},
		{"the console, forwarded as another host", console, console, "api.example.com", http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reached := false
			handler := accessgate.Host(c.configured, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				reached = true
				w.WriteHeader(http.StatusOK)
			}))

			request := httptest.NewRequest(http.MethodGet, "/customers", http.NoBody)
			request.Host = c.host
			if c.forwarded != "" {
				request.Header.Set("X-Forwarded-Host", c.forwarded)
				request.Header.Set("Forwarded", "host="+c.forwarded)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)

			require.Equal(t, c.status, recorder.Code)
			require.Equal(t, c.status == http.StatusOK, reached)
			if c.status == http.StatusNotFound {
				require.Empty(t, recorder.Body.Bytes(), "a refusal has no body")
			}
		})
	}
}

func Test_ValidHost(t *testing.T) {
	for _, host := range []string{"console.example.com", "Console.Example.COM", "localhost", "console-1.internal"} {
		t.Run("accepts "+host, func(t *testing.T) {
			require.NoError(t, accessgate.ValidHost(host))
		})
	}
	for name, host := range map[string]string{
		"nothing":            "",
		"a blank":            " ",
		"a scheme":           "https://console.example.com",
		"a port":             "console.example.com:8443",
		"a trailing dot":     "console.example.com.",
		"a path":             "console.example.com/customers",
		"a trailing slash":   "console.example.com/",
		"a query":            "console.example.com?x=1",
		"a user":             "user@console.example.com",
		"a leading space":    " console.example.com",
		"a trailing space":   "console.example.com ",
		"a trailing newline": "console.example.com\n",
		"an IPv6 literal":    "[::1]",
		"only a port":        ":8443",
	} {
		t.Run("refuses "+name, func(t *testing.T) {
			err := accessgate.ValidHost(host)
			require.Error(t, err)
			if len(host) > 3 {
				require.NotContains(t, err.Error(), host, "the value is not echoed")
			}
		})
	}
}
