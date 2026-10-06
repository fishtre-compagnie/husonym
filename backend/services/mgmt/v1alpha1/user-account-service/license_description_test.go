package v1alpha1_useraccountservice

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/fishtre-compagnie/husonym/backend/internal/licensestore"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

// heldLicense is what the process holds, set by the test. It is safe for concurrent use.
type heldLicense struct {
	mu          sync.Mutex
	description license.Description
}

func (h *heldLicense) Describe() license.Description {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.description
}

func (h *heldLicense) holds(description license.Description) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.description = description
}

// installationsStore answers how keys were stored, by license id, and records what it was
// asked. It is safe for concurrent use. Nothing else of a store is used by a description.
type installationsStore struct {
	mu    sync.Mutex
	rows  map[string]*licensestore.Installation
	err   error
	asked []string
}

func (s *installationsStore) Installation(_ context.Context, licenseId string) (*licensestore.Installation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asked = append(s.asked, licenseId)
	if s.err != nil {
		return nil, s.err
	}
	return s.rows[licenseId], nil
}

func (s *installationsStore) Offer(context.Context, string, licensestore.Origin, *pgtype.UUID) (*licensestore.Result, error) {
	return nil, errors.New("a description offers no key")
}

func (s *installationsStore) Current(context.Context) (string, error) {
	return "", errors.New("a description does not read the key value")
}

func (s *installationsStore) fails(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

func (s *installationsStore) askedFor() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.asked...)
}

type descriptionFixture struct {
	service   *Service
	held      *heldLicense
	store     *installationsStore
	refreshes int
	// refreshErr is what the refresh answers.
	refreshErr error
}

// newDescriptionFixture gives a service with no license client at all: a description that
// read it, instead of the one snapshot, would not get through the test.
func newDescriptionFixture() *descriptionFixture {
	f := &descriptionFixture{
		held:  &heldLicense{description: license.Description{State: license.StateNone}},
		store: &installationsStore{rows: map[string]*licensestore.Installation{}},
	}
	f.service = New(&Config{}, nil, nil, nil, nil, nil, nil, f.held, f.store, func(context.Context) error {
		f.refreshes++
		return f.refreshErr
	})
	return f
}

func validKey(id string) license.Description {
	return license.Description{
		State: license.StateValid,
		Key:   &license.Key{Id: id, ExpiresAt: time.Now().UTC().Add(90 * 24 * time.Hour).Truncate(time.Second)},
	}
}

func Test_SystemLicense_TellsTheInstallationOfTheKeyItDescribes(t *testing.T) {
	ctx := t.Context()
	firstStored := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Second)
	secondStored := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)

	t.Run("without a key the store is not asked", func(t *testing.T) {
		f := newDescriptionFixture()

		described := f.service.systemLicense(ctx)

		require.Empty(t, described.GetOrigin())
		require.Nil(t, described.GetInstalledAt())
		require.Empty(t, f.store.askedFor())
	})

	t.Run("the store is asked once per key, by the id of the key described", func(t *testing.T) {
		f := newDescriptionFixture()
		f.store.rows["lic-1"] = &licensestore.Installation{Origin: licensestore.OriginFile, At: firstStored}
		f.store.rows["lic-2"] = &licensestore.Installation{Origin: licensestore.OriginInterface, At: secondStored}

		// A newer key is already stored, but this process still holds the first: what it
		// tells is the installation of the key it describes.
		f.held.holds(validKey("lic-1"))
		for range 3 {
			described := f.service.systemLicense(ctx)
			require.Equal(t, "file", described.GetOrigin())
			require.True(t, firstStored.Equal(described.GetInstalledAt().AsTime()))
		}
		require.Equal(t, []string{"lic-1"}, f.store.askedFor())

		f.held.holds(validKey("lic-2"))
		for range 3 {
			described := f.service.systemLicense(ctx)
			require.Equal(t, "interface", described.GetOrigin())
			require.True(t, secondStored.Equal(described.GetInstalledAt().AsTime()))
		}
		require.Equal(t, []string{"lic-1", "lic-2"}, f.store.askedFor())
	})

	t.Run("a store that does not answer leaves the two fields empty, and is asked again", func(t *testing.T) {
		f := newDescriptionFixture()
		f.store.rows["lic-1"] = &licensestore.Installation{Origin: licensestore.OriginEnvironment, At: firstStored}
		f.held.holds(validKey("lic-1"))
		f.store.fails(errors.New("connection reset"))

		described := f.service.systemLicense(ctx)
		// The rest of the description is there all the same.
		require.True(t, described.GetIsValid())
		require.Equal(t, "valid", described.GetState())
		require.Empty(t, described.GetOrigin())
		require.Nil(t, described.GetInstalledAt())

		f.store.fails(nil)
		described = f.service.systemLicense(ctx)
		require.Equal(t, "environment", described.GetOrigin())
		require.True(t, firstStored.Equal(described.GetInstalledAt().AsTime()))
		require.Equal(t, []string{"lic-1", "lic-1"}, f.store.askedFor())
	})

	t.Run("a key the store does not hold is remembered as such", func(t *testing.T) {
		f := newDescriptionFixture()
		f.held.holds(validKey("lic-unknown"))

		for range 3 {
			require.Empty(t, f.service.systemLicense(ctx).GetOrigin())
		}
		require.Equal(t, []string{"lic-unknown"}, f.store.askedFor())
	})

	t.Run("calls that arrive together all get the answer", func(t *testing.T) {
		f := newDescriptionFixture()
		f.store.rows["lic-1"] = &licensestore.Installation{Origin: licensestore.OriginFile, At: firstStored}
		f.held.holds(validKey("lic-1"))

		var wg sync.WaitGroup
		for range 20 {
			wg.Go(func() {
				for range 50 {
					if origin := f.service.systemLicense(ctx).GetOrigin(); origin != "file" {
						t.Errorf("origin is %q", origin)
					}
				}
			})
		}
		wg.Wait()
		// Those that arrived before the first answer each asked; nobody asks after it.
		require.LessOrEqual(t, len(f.store.askedFor()), 20)
	})
}

func Test_SystemLicense_ComesFromOneDescription(t *testing.T) {
	ctx := t.Context()
	expiresAt := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Second)

	for state, valid := range map[license.State]bool{
		license.StateValid:    true,
		license.StateExpiring: true,
		license.StateGrace:    true,
		license.StateFrozen:   false,
	} {
		t.Run("a key in state "+string(state), func(t *testing.T) {
			f := newDescriptionFixture()
			f.held.holds(license.Description{State: state, Key: &license.Key{Id: "lic-1", ExpiresAt: expiresAt}})

			described := f.service.systemLicense(ctx)

			require.Equal(t, string(state), described.GetState())
			require.Equal(t, valid, described.GetIsValid())
			// The expiry is the key's own, whatever the state.
			require.True(t, expiresAt.Equal(described.GetExpiresAt().AsTime()))
		})
	}

	t.Run("without a key the expiry is the present instant", func(t *testing.T) {
		f := newDescriptionFixture()

		described := f.service.systemLicense(ctx)

		require.Equal(t, "none", described.GetState())
		require.False(t, described.GetIsValid())
		require.WithinDuration(t, time.Now(), described.GetExpiresAt().AsTime(), time.Minute)
	})
}

func Test_SystemLicense_Problem(t *testing.T) {
	ctx := t.Context()

	t.Run("a key that could not be loaded is told without what the database said", func(t *testing.T) {
		f := newDescriptionFixture()
		driver := errors.New("dial tcp db.internal:5432: password authentication failed for user husonym_admin")
		f.held.holds(license.Description{
			State:   license.StateNone,
			Problem: fmt.Errorf("%w: %w", license.ErrKeyNotLoaded, driver),
		})

		described := f.service.systemLicense(ctx)

		require.Equal(t, "the license key could not be loaded", described.GetProblem())
	})

	t.Run("a key that was refused is told with the reason of the refusal", func(t *testing.T) {
		f := newDescriptionFixture()
		_, refusal := license.ParseWith("not-a-key", license.Keyring{})
		require.Error(t, refusal)
		f.held.holds(license.Description{State: license.StateNone, Problem: refusal})

		described := f.service.systemLicense(ctx)

		require.Equal(t, refusal.Error(), described.GetProblem())
	})

	t.Run("no problem is no field", func(t *testing.T) {
		f := newDescriptionFixture()
		require.Nil(t, f.service.systemLicense(ctx).Problem)
	})
}

func Test_LicenseAfterOffer(t *testing.T) {
	ctx := t.Context()

	t.Run("an accepted key that cannot be read back is answered, not failed", func(t *testing.T) {
		f := newDescriptionFixture()
		f.refreshErr = errors.New("connection reset")
		f.held.holds(validKey("lic-1"))

		described, err := f.service.licenseAfterOffer(ctx, &licensestore.Result{Outcome: licensestore.Accepted})

		require.NoError(t, err)
		require.Equal(t, "valid", described.GetState())
		require.Equal(t, 1, f.refreshes)
	})

	t.Run("an accepted key is read back before the answer", func(t *testing.T) {
		f := newDescriptionFixture()

		_, err := f.service.licenseAfterOffer(ctx, &licensestore.Result{Outcome: licensestore.Accepted})

		require.NoError(t, err)
		require.Equal(t, 1, f.refreshes)
	})

	t.Run("the key already in force is answered without reading anything again", func(t *testing.T) {
		f := newDescriptionFixture()
		f.held.holds(validKey("lic-1"))

		described, err := f.service.licenseAfterOffer(ctx, &licensestore.Result{Outcome: licensestore.Unchanged})

		require.NoError(t, err)
		require.Equal(t, "valid", described.GetState())
		require.Zero(t, f.refreshes)
	})

	t.Run("a refusal is an error carrying its reason, and nothing is read again", func(t *testing.T) {
		for outcome, code := range map[licensestore.Outcome]connect.Code{
			licensestore.RefusedInvalid: connect.CodeInvalidArgument,
			licensestore.RefusedOlder:   connect.CodeFailedPrecondition,
		} {
			f := newDescriptionFixture()

			described, err := f.service.licenseAfterOffer(ctx, &licensestore.Result{Outcome: outcome, Reason: "the reason"})

			require.Nil(t, described)
			require.Equal(t, code, connect.CodeOf(err))
			require.ErrorContains(t, err, "the reason")
			require.Zero(t, f.refreshes)
		}
	})
}
