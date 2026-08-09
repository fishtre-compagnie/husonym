package license

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A license with no caps to carry, standing in for the cloud license and the test fakes
// that implement EEInterface but not LimitedLicense.
type unlimitedStub struct {
	valid     bool
	expiresAt time.Time
}

func (s *unlimitedStub) IsValid() bool        { return s.valid }
func (s *unlimitedStub) ExpiresAt() time.Time { return s.expiresAt }

type limitedStub struct {
	unlimitedStub
	limits *Limits
}

func (s *limitedStub) Limits() *Limits { return s.limits }

func Test_NewCascadeLicense(t *testing.T) {
	future := time.Now().UTC().Add(24 * time.Hour)
	past := time.Now().UTC().Add(-24 * time.Hour)

	t.Run("no licenses at all", func(t *testing.T) {
		c := NewCascadeLicense()
		require.False(t, c.IsValid())
		require.Nil(t, c.Limits())
	})

	t.Run("all invalid", func(t *testing.T) {
		c := NewCascadeLicense(
			&unlimitedStub{valid: false, expiresAt: past},
			&limitedStub{unlimitedStub{valid: false, expiresAt: past}, &Limits{MaxJobs: ptr(9)}},
		)
		require.False(t, c.IsValid())
		// Caps must not leak from a license that grants nothing.
		require.Nil(t, c.Limits())
	})

	t.Run("first valid entry wins", func(t *testing.T) {
		first := &limitedStub{unlimitedStub{valid: true, expiresAt: future}, &Limits{MaxJobs: ptr(1)}}
		second := &limitedStub{unlimitedStub{valid: true, expiresAt: future}, &Limits{MaxJobs: ptr(2)}}
		c := NewCascadeLicense(first, second)
		require.True(t, c.IsValid())
		require.Equal(t, 1, *c.Limits().MaxJobs)
	})

	t.Run("skips invalid entries and takes the caps of the one that granted access", func(t *testing.T) {
		c := NewCascadeLicense(
			&unlimitedStub{valid: false, expiresAt: past},
			&limitedStub{unlimitedStub{valid: true, expiresAt: future}, &Limits{MaxConnections: ptr(3)}},
		)
		require.True(t, c.IsValid())
		require.NotNil(t, c.Limits())
		require.Equal(t, 3, *c.Limits().MaxConnections)
	})

	// The cloud license and the test fakes do not implement LimitedLicense; that must
	// read as "uncapped", never as a nil dereference.
	t.Run("a license without caps yields nil limits", func(t *testing.T) {
		c := NewCascadeLicense(&unlimitedStub{valid: true, expiresAt: future})
		require.True(t, c.IsValid())
		require.Nil(t, c.Limits())
		require.True(t, c.Limits().Allows("postgres"))
	})
}

func Test_LimitsOf(t *testing.T) {
	t.Run("returns nil for a license that carries none", func(t *testing.T) {
		require.Nil(t, LimitsOf(&unlimitedStub{valid: true}))
	})

	t.Run("returns the caps when carried", func(t *testing.T) {
		l := LimitsOf(&limitedStub{unlimitedStub{valid: true}, &Limits{MaxJobs: ptr(7)}})
		require.NotNil(t, l)
		require.Equal(t, 7, *l.MaxJobs)
	})

	t.Run("reads through an EELicense", func(t *testing.T) {
		ee := newFromLicenseContents(&licenseContents{
			Version: "v1", Id: "1",
			ExpiresAt: time.Now().UTC().Add(time.Hour),
			Limits:    &Limits{MaxJobs: ptr(4)},
		})
		l := LimitsOf(ee)
		require.NotNil(t, l)
		require.Equal(t, 4, *l.MaxJobs)
	})
}
