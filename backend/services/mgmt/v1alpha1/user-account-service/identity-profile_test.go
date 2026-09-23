package v1alpha1_useraccountservice

import (
	"testing"

	"github.com/fishtre-compagnie/husonym/internal/authmgmt"
	"github.com/stretchr/testify/require"
)

func Test_isDisplayIdentityComplete(t *testing.T) {
	cases := []struct {
		name     string
		identity *authmgmt.User
		want     bool
	}{
		{"all three fields", &authmgmt.User{Name: "foo", Email: "bar", Picture: "baz"}, true},
		{"no picture", &authmgmt.User{Name: "foo", Email: "bar"}, false},
		{"no name", &authmgmt.User{Email: "bar", Picture: "baz"}, false},
		{"no address", &authmgmt.User{Name: "foo", Picture: "baz"}, false},
		{"nothing at all", &authmgmt.User{}, false},
		{"nil", nil, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, isDisplayIdentityComplete(tc.identity))
		})
	}
}

func Test_completeDisplayIdentity(t *testing.T) {
	// The case the review found: the three columns are written independently, so a token
	// that carried an address but no name leaves a row that must not be taken for a
	// complete profile -- the fallback still has the rest.
	t.Run("fills the blanks a sparse token left", func(t *testing.T) {
		got := completeDisplayIdentity(
			&authmgmt.User{Email: "ada@example.com"},
			&authmgmt.User{Name: "Ada Lovelace", Email: "stale@example.com", Picture: "https://example.com/ada.png"},
		)

		require.Equal(t, "Ada Lovelace", got.Name)
		require.Equal(t, "https://example.com/ada.png", got.Picture)
		require.Equal(
			t,
			"ada@example.com",
			got.Email,
			"what the provider asserted at sign-in is the fresher statement and wins",
		)
	})

	t.Run("the fallback may only ever add", func(t *testing.T) {
		stored := &authmgmt.User{Name: "Ada King", Email: "ada.king@example.com", Picture: "https://example.com/king.png"}
		got := completeDisplayIdentity(
			stored,
			&authmgmt.User{Name: "Ada Lovelace", Email: "ada@example.com", Picture: "https://example.com/ada.png"},
		)

		require.Equal(t, stored.Name, got.Name)
		require.Equal(t, stored.Email, got.Email)
		require.Equal(t, stored.Picture, got.Picture)
	})

	t.Run("a user who never signed in takes the fallback whole", func(t *testing.T) {
		fallback := &authmgmt.User{Name: "Ada Lovelace", Email: "ada@example.com", Picture: "https://example.com/ada.png"}
		got := completeDisplayIdentity(&authmgmt.User{}, fallback)

		require.Equal(t, fallback.Name, got.Name)
		require.Equal(t, fallback.Email, got.Email)
		require.Equal(t, fallback.Picture, got.Picture)
	})

	t.Run("no fallback keeps what was stored", func(t *testing.T) {
		stored := &authmgmt.User{Email: "ada@example.com"}
		got := completeDisplayIdentity(stored, nil)

		require.Equal(t, "ada@example.com", got.Email)
		require.Empty(t, got.Name)
	})

	// EmailVerified is not a display field: it says whether the provider vouched for the
	// address, and an administration API answering about a different source must not be
	// able to raise it.
	t.Run("the verified assertion never comes from the fallback", func(t *testing.T) {
		got := completeDisplayIdentity(
			&authmgmt.User{Email: "ada@example.com", EmailVerified: false},
			&authmgmt.User{Email: "ada@example.com", EmailVerified: true},
		)

		require.False(t, got.EmailVerified)
	})
}
