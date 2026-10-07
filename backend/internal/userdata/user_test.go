package userdata

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func Test_User_EnforceFeature(t *testing.T) {
	ctx := context.Background()
	accountId := uuid.NewString()
	userWith := func(t *testing.T, lic license.EEInterface) *User {
		t.Helper()
		user, err := NewClient(fakeUserService{userId: uuid.NewString()}, allowsEverything{}, lic).GetUser(ctx)
		require.NoError(t, err)
		return user
	}

	t.Run("an invalid license is refused as EnforceLicense refuses it", func(t *testing.T) {
		user := userWith(t, testutil.NewFakeEELicense(testutil.WithFeatures(license.FeatureMcp)))
		want := user.EnforceLicense(ctx, accountId)
		require.Error(t, want)
		require.Equal(t, want, user.EnforceFeature(ctx, accountId, license.FeatureMcp))
	})

	t.Run("a valid license without the feature is forbidden, naming it", func(t *testing.T) {
		user := userWith(t, testutil.NewFakeEELicense(testutil.WithIsValid(), testutil.WithFeatures(license.FeatureMcp)))
		err := user.EnforceFeature(ctx, accountId, license.FeatureSso)
		require.ErrorContains(t, err, "this license does not include sso")
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	})

	t.Run("a valid license with the feature passes", func(t *testing.T) {
		user := userWith(t, testutil.NewFakeEELicense(testutil.WithIsValid(), testutil.WithFeatures(license.FeatureMcp)))
		require.NoError(t, user.EnforceFeature(ctx, accountId, license.FeatureMcp))
		require.True(t, user.HasFeature(license.FeatureMcp))
		require.False(t, user.HasFeature(license.FeatureSso))
	})

	t.Run("a user without a license has no feature", func(t *testing.T) {
		require.False(t, (&User{}).HasFeature(license.FeatureMcp))
	})
}

// A service that holds the license and no user answers what EnforceFeature answers.
func Test_FeatureRefusal(t *testing.T) {
	ctx := context.Background()
	accountId := uuid.NewString()
	for name, lic := range map[string]*testutil.FakeEELicense{
		"a license that is not in force":         testutil.NewFakeEELicense(testutil.WithFeatures(license.FeatureMcp)),
		"a license in force without the feature": testutil.NewFakeEELicense(testutil.WithIsValid(), testutil.WithFeatures(license.FeatureSso)),
		"a license in force with the feature":    testutil.NewFakeEELicense(testutil.WithIsValid(), testutil.WithFeatures(license.FeatureMcp)),
	} {
		t.Run(name, func(t *testing.T) {
			user, err := NewClient(fakeUserService{userId: uuid.NewString()}, allowsEverything{}, lic).GetUser(ctx)
			require.NoError(t, err)
			require.Equal(t, user.EnforceFeature(ctx, accountId, license.FeatureMcp), FeatureRefusal(lic, license.FeatureMcp))
		})
	}

	t.Run("no license at all is not in force", func(t *testing.T) {
		err := FeatureRefusal(nil, license.FeatureMcp)
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
		require.ErrorContains(t, err, "account does not have an active license")
	})
}
