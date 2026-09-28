package userdata

import (
	"testing"

	auth_apikey "github.com/fishtre-compagnie/husonym/backend/internal/auth/apikey"
	"github.com/fishtre-compagnie/husonym/internal/apikey"
	"github.com/stretchr/testify/require"
)

// What only the worker calls — the context of a run, the consistency key, the reconciled
// mappings — is the worker's: with its own key, that key alone; without one, an API key,
// never the session of a person; without authentication, anyone, since nothing is told apart.
func Test_WorkerOnly_Allow(t *testing.T) {
	callers := map[string]*User{
		"worker key":  {apiKeyData: &auth_apikey.TokenContextData{ApiKeyType: apikey.WorkerApiKey}},
		"account key": {apiKeyData: &auth_apikey.TokenContextData{ApiKeyType: apikey.AccountApiKey}},
		"session":     {},
	}
	for name, tc := range map[string]struct {
		guard   WorkerOnly
		allowed map[string]bool
	}{
		"without authentication": {
			guard:   WorkerOnly{},
			allowed: map[string]bool{"worker key": true, "account key": true, "session": true},
		},
		"authentication, no worker key": {
			guard:   WorkerOnly{IsAuthEnabled: true},
			allowed: map[string]bool{"worker key": true, "account key": true, "session": false},
		},
		"authentication and a worker key": {
			guard:   WorkerOnly{IsAuthEnabled: true, HasWorkerApiKeys: true},
			allowed: map[string]bool{"worker key": true, "account key": false, "session": false},
		},
		"cloud": {
			guard:   WorkerOnly{IsAuthEnabled: true, IsHusonymCloud: true},
			allowed: map[string]bool{"worker key": true, "account key": false, "session": false},
		},
	} {
		t.Run(name, func(t *testing.T) {
			for who, user := range callers {
				err := tc.guard.Allow(user)
				if tc.allowed[who] {
					require.NoError(t, err, who)
				} else {
					require.Error(t, err, who)
				}
			}
		})
	}
}
