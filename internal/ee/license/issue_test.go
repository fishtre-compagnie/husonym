package license

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func Test_Issue(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	// The property that matters: whatever Issue mints, getLicense must accept and read
	// back identically. Issuing and verifying share their structures precisely so this
	// cannot drift.
	t.Run("round-trips through verification", func(t *testing.T) {
		expires := time.Now().UTC().Add(365 * 24 * time.Hour)
		issued, err := Issue(IssueRequest{
			IssuedTo:   "Acme Co.",
			CustomerId: "cust-001",
			ExpiresAt:  expires,
			GraceDays:  ptr(30),
			Limits:     &Limits{MaxJobs: ptr(10), AllowedConnectionTypes: []string{"postgres"}},
		}, priv)
		require.NoError(t, err)
		require.NotEmpty(t, issued.Encoded)
		require.NotEmpty(t, issued.Id)

		got, err := getLicense(issued.Encoded, pub)
		require.NoError(t, err)
		require.Equal(t, "Acme Co.", got.IssuedTo)
		require.Equal(t, "cust-001", got.CustomerId)
		require.Equal(t, expires.Truncate(time.Second), got.ExpiresAt)
		require.Equal(t, 30, *got.GraceDays)
		require.Equal(t, 10, *got.Limits.MaxJobs)
		require.True(t, got.Limits.Allows("postgres"))
		require.False(t, got.Limits.Allows("mssql"))
		require.True(t, got.IsValid())
		require.Equal(t, StateValid, got.State())
	})

	t.Run("generates a distinct id when none is given", func(t *testing.T) {
		req := IssueRequest{IssuedTo: "A", CustomerId: "c", ExpiresAt: time.Now().UTC().Add(time.Hour)}
		first, err := Issue(req, priv)
		require.NoError(t, err)
		second, err := Issue(req, priv)
		require.NoError(t, err)
		require.NotEqual(t, first.Id, second.Id)
	})

	t.Run("honours an explicit id", func(t *testing.T) {
		issued, err := Issue(IssueRequest{
			Id: "contract-42", IssuedTo: "A", CustomerId: "c",
			ExpiresAt: time.Now().UTC().Add(time.Hour),
		}, priv)
		require.NoError(t, err)
		require.Equal(t, "contract-42", issued.Id)
	})

	t.Run("rejects invalid requests", func(t *testing.T) {
		valid := time.Now().UTC().Add(time.Hour)
		cases := map[string]IssueRequest{
			"no issued_to":   {CustomerId: "c", ExpiresAt: valid},
			"no customer_id": {IssuedTo: "A", ExpiresAt: valid},
			"no expiry":      {IssuedTo: "A", CustomerId: "c"},
			"expiry in past": {IssuedTo: "A", CustomerId: "c", ExpiresAt: time.Now().UTC().Add(-time.Minute)},
			"negative grace": {IssuedTo: "A", CustomerId: "c", ExpiresAt: valid, GraceDays: ptr(-1)},
			"negative max":   {IssuedTo: "A", CustomerId: "c", ExpiresAt: valid, Limits: &Limits{MaxJobs: ptr(-1)}},
		}
		for name, req := range cases {
			t.Run(name, func(t *testing.T) {
				issued, err := Issue(req, priv)
				require.Error(t, err)
				require.Nil(t, issued)
			})
		}
	})

	t.Run("rejects a missing key", func(t *testing.T) {
		issued, err := Issue(IssueRequest{
			IssuedTo: "A", CustomerId: "c", ExpiresAt: time.Now().UTC().Add(time.Hour),
		}, nil)
		require.Error(t, err)
		require.Nil(t, issued)
	})

	// A license minted with the wrong key is the failure mode that would only surface at
	// the customer's site, so it must be impossible to miss.
	t.Run("a license from another key does not verify", func(t *testing.T) {
		_, otherPriv, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)
		issued, err := Issue(IssueRequest{
			IssuedTo: "A", CustomerId: "c", ExpiresAt: time.Now().UTC().Add(time.Hour),
		}, otherPriv)
		require.NoError(t, err)

		got, err := getLicense(issued.Encoded, pub)
		require.Error(t, err)
		require.Nil(t, got)
	})
}

func Test_Registry(t *testing.T) {
	newEntry := func(id string, expiresIn time.Duration, grace *int) RegistryEntry {
		return RegistryEntry{
			Id:         id,
			IssuedTo:   "Customer " + id,
			CustomerId: "cust-" + id,
			IssuedAt:   time.Now().UTC(),
			ExpiresAt:  time.Now().UTC().Add(expiresIn),
			GraceDays:  grace,
			Encoded:    "encoded-" + id,
		}
	}

	t.Run("a missing file loads as empty", func(t *testing.T) {
		r, err := LoadRegistry(filepath.Join(t.TempDir(), "nope.json"))
		require.NoError(t, err)
		require.Empty(t, r.Entries)
	})

	t.Run("round-trips through disk", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "registry.json")
		r, err := LoadRegistry(path)
		require.NoError(t, err)
		require.NoError(t, r.Add(newEntry("a", 30*24*time.Hour, ptr(7))))
		require.NoError(t, r.Save(path))

		reloaded, err := LoadRegistry(path)
		require.NoError(t, err)
		require.Len(t, reloaded.Entries, 1)
		require.Equal(t, "Customer a", reloaded.Entries[0].IssuedTo)
		require.Equal(t, 7, *reloaded.Entries[0].GraceDays)
	})

	t.Run("refuses duplicate ids", func(t *testing.T) {
		r := &Registry{}
		require.NoError(t, r.Add(newEntry("dup", time.Hour, nil)))
		require.Error(t, r.Add(newEntry("dup", time.Hour, nil)))
		require.Len(t, r.Entries, 1)
	})

	t.Run("is written with restrictive permissions", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "nested", "registry.json")
		r := &Registry{}
		require.NoError(t, r.Add(newEntry("a", time.Hour, nil)))
		require.NoError(t, r.Save(path))

		info, err := os.Stat(path)
		require.NoError(t, err)
		require.Equal(t, "-rw-------", info.Mode().String(), "the registry holds customer data")
	})

	t.Run("expiring worklist is sorted and excludes frozen", func(t *testing.T) {
		r := &Registry{}
		require.NoError(t, r.Add(newEntry("soon", 5*24*time.Hour, nil)))
		require.NoError(t, r.Add(newEntry("later", 20*24*time.Hour, nil)))
		require.NoError(t, r.Add(newEntry("far", 300*24*time.Hour, nil)))
		// Expired well beyond its grace: past saving by a reminder.
		require.NoError(t, r.Add(newEntry("gone", -60*24*time.Hour, ptr(14))))
		// Expired but still in grace: very much actionable.
		require.NoError(t, r.Add(newEntry("grace", -time.Hour, ptr(14))))

		got := r.ExpiringWithin(45 * 24 * time.Hour)
		ids := []string{}
		for _, e := range got {
			ids = append(ids, e.Id)
		}
		require.Equal(t, []string{"grace", "soon", "later"}, ids)

		frozen := r.Frozen()
		require.Len(t, frozen, 1)
		require.Equal(t, "gone", frozen[0].Id)
	})

	t.Run("lists a customer's renewal chain", func(t *testing.T) {
		r := &Registry{}
		a := newEntry("y2", 400*24*time.Hour, nil)
		a.CustomerId = "acme"
		b := newEntry("y1", 30*24*time.Hour, nil)
		b.CustomerId = "acme"
		other := newEntry("other", time.Hour, nil)
		require.NoError(t, r.Add(a))
		require.NoError(t, r.Add(b))
		require.NoError(t, r.Add(other))

		chain := r.ForCustomer("acme")
		require.Len(t, chain, 2)
		require.Equal(t, "y1", chain[0].Id, "soonest expiry first")
	})

	t.Run("entry reports its lifecycle state", func(t *testing.T) {
		require.Equal(t, StateValid, newEntry("a", 90*24*time.Hour, nil).State())
		require.Equal(t, StateExpiring, newEntry("b", 5*24*time.Hour, nil).State())
		require.Equal(t, StateGrace, newEntry("c", -time.Hour, nil).State())
		require.Equal(t, StateFrozen, newEntry("d", -60*24*time.Hour, nil).State())
	})
}
