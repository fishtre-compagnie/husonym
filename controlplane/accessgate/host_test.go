package accessgate_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fishtre-compagnie/husonym/controlplane/accessgate"
)

func Test_Host(t *testing.T) {
	cases := []struct {
		name       string
		configured string
		host       string
		status     int
	}{
		{"the host of the console", "console.example.com", "console.example.com", http.StatusOK},
		{"with a port", "console.example.com", "console.example.com:8443", http.StatusOK},
		{"in upper case", "console.example.com", "CONSOLE.Example.COM", http.StatusOK},
		{"in upper case with a port", "console.example.com", "CONSOLE.EXAMPLE.COM:443", http.StatusOK},
		{"configured in upper case", "Console.Example.com", "console.example.com", http.StatusOK},
		{"another host", "console.example.com", "api.example.com", http.StatusNotFound},
		{"another host with a port", "console.example.com", "api.example.com:8443", http.StatusNotFound},
		{"a host that only ends like it", "console.example.com", "evil-console.example.com", http.StatusNotFound},
		{"a host that only starts like it", "console.example.com", "console.example.com.evil.test", http.StatusNotFound},
		{"an empty host", "console.example.com", "", http.StatusNotFound},
		{"only a port", "console.example.com", ":8443", http.StatusNotFound},
		{"an empty host when none is configured", "", "", http.StatusNotFound},
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
