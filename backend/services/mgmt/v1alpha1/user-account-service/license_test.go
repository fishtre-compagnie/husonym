package v1alpha1_useraccountservice_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1/mgmtv1alpha1connect"
	tchusonymapi "github.com/fishtre-compagnie/husonym/backend/pkg/integration-test"
	"github.com/fishtre-compagnie/husonym/internal/apikey"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/fishtre-compagnie/husonym/internal/license"
	"github.com/fishtre-compagnie/husonym/internal/rbac"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
)

// licenseWorld is an API whose license is the key its database holds, with an administrator
// of a team account: the license belongs to the instance, and the account is whose permissions
// are asked for.
type licenseWorld struct {
	api       *tchusonymapi.HusonymApiTestClient
	admin     mgmtv1alpha1connect.UserAccountServiceClient
	adminId   string
	accountId string
}

func newLicenseWorld(ctx context.Context, t *testing.T) *licenseWorld {
	t.Helper()
	api, err := tchusonymapi.NewHusonymApiTestClient(ctx, t)
	require.NoError(t, err)
	t.Cleanup(func() { _ = api.TearDown(context.Background()) })

	admin := api.OSSAuthenticatedStoredLicenseClients.Users(tchusonymapi.WithUserId("license-admin"))
	adminId := tchusonymapi.SetUser(ctx, t, admin)
	return &licenseWorld{
		api:       api,
		admin:     admin,
		adminId:   adminId,
		accountId: tchusonymapi.CreateTeamAccount(ctx, t, admin, "license-team"),
	}
}

// issue signs a key the API of the test accepts. The issue date is given because it is what
// the API orders keys by.
func (w *licenseWorld) issue(t *testing.T, issuedAt time.Time, shape func(*license.IssueRequest)) string {
	t.Helper()
	req := &license.IssueRequest{
		IssuedTo:   "Acme Co.",
		CustomerId: "cust-001",
		IssuedAt:   issuedAt,
		ExpiresAt:  time.Now().UTC().Add(90 * 24 * time.Hour),
	}
	if shape != nil {
		shape(req)
	}
	issued, err := license.Issue(req, w.api.LicenseSigningKey, w.api.LicenseKeyring)
	require.NoError(t, err)
	return issued.Encoded
}

// set gives a key through the team account of the world.
func (w *licenseWorld) set(
	ctx context.Context,
	client mgmtv1alpha1connect.UserAccountServiceClient,
	key string,
) (*connect.Response[mgmtv1alpha1.SetSystemLicenseResponse], error) {
	return setThrough(ctx, client, w.accountId, key)
}

// setThrough gives a key through the account the caller names.
func setThrough(
	ctx context.Context,
	client mgmtv1alpha1connect.UserAccountServiceClient,
	accountId, key string,
) (*connect.Response[mgmtv1alpha1.SetSystemLicenseResponse], error) {
	return client.SetSystemLicense(ctx, connect.NewRequest(&mgmtv1alpha1.SetSystemLicenseRequest{
		AccountId: accountId,
		Key:       key,
	}))
}

func (w *licenseWorld) described(ctx context.Context, t *testing.T) *mgmtv1alpha1.SystemLicense {
	t.Helper()
	resp, err := w.admin.GetSystemInformation(ctx, connect.NewRequest(&mgmtv1alpha1.GetSystemInformationRequest{}))
	require.NoError(t, err)
	return resp.Msg.GetLicense()
}

// viewer gives a second member of the account, who may only look.
func (w *licenseWorld) viewer(ctx context.Context, t *testing.T) mgmtv1alpha1connect.UserAccountServiceClient {
	t.Helper()
	viewer := w.api.OSSAuthenticatedStoredLicenseClients.Users(tchusonymapi.WithUserId("license-viewer"))
	viewerId := tchusonymapi.SetUser(ctx, t, viewer)
	// Joining goes through an invitation and its e-mail; the membership is written directly.
	accountUuid, err := husonymdb.ToUuid(w.accountId)
	require.NoError(t, err)
	viewerUuid, err := husonymdb.ToUuid(viewerId)
	require.NoError(t, err)
	require.NoError(t, w.api.HusonymQuerier.CreateAccountUserAssociation(ctx, w.api.Pgcontainer.DB,
		db_queries.CreateAccountUserAssociationParams{AccountID: accountUuid, UserID: viewerUuid}))
	// The role is written directly too: giving a role other than administrator through the API
	// asks for the rbac feature, and this world starts without a license in force.
	roles, err := rbac.New(ctx, w.api.Pgcontainer.DB, testutil.GetTestLogger(t))
	require.NoError(t, err)
	require.NoError(t, roles.SetRole(
		ctx, rbac.NewUser(viewerId), rbac.NewAccount(w.accountId), mgmtv1alpha1.AccountRole_ACCOUNT_ROLE_JOB_VIEWER,
	))
	return viewer
}

func Test_SetSystemLicense(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	w := newLicenseWorld(ctx, t)
	now := time.Now().UTC().Truncate(time.Second)
	older := w.issue(t, now.Add(-2*time.Hour), nil)
	key := w.issue(t, now.Add(-time.Hour), nil)

	// The steps follow one another on one instance: each starts from what the one before left.
	t.Run("an instance starts without a license", func(t *testing.T) {
		described := w.described(ctx, t)
		require.Equal(t, "none", described.GetState())
		require.False(t, described.GetIsValid())
	})

	viewer := w.viewer(ctx, t)
	t.Run("a job viewer is refused, and nothing is stored", func(t *testing.T) {
		_, err := w.set(ctx, viewer, key)
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
		require.Equal(t, "none", w.described(ctx, t).GetState())
	})

	t.Run("what is not a key is an invalid argument that does not repeat it", func(t *testing.T) {
		_, err := w.set(ctx, w.admin, "not-a-key")
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
		require.NotContains(t, err.Error(), "not-a-key")
		require.Equal(t, "none", w.described(ctx, t).GetState())
	})

	t.Run("a key is accepted without a license in force, and is in force at once", func(t *testing.T) {
		resp, err := w.set(ctx, w.admin, key)
		require.NoError(t, err)
		answered := resp.Msg.GetLicense()
		require.True(t, answered.GetIsValid())
		require.Equal(t, "valid", answered.GetState())
		require.Equal(t, "interface", answered.GetOrigin())
		require.WithinDuration(t, time.Now(), answered.GetInstalledAt().AsTime(), time.Minute)

		// The answer describes the key and never gives it back.
		raw, err := protojson.Marshal(resp.Msg)
		require.NoError(t, err)
		require.NotContains(t, string(raw), key)

		// No waiting for the background refresh: the next call already sees it.
		described := w.described(ctx, t)
		require.True(t, described.GetIsValid())
		require.Equal(t, "valid", described.GetState())

		// It is stored as given by the person who gave it.
		stored, err := w.api.HusonymQuerier.GetCurrentLicenseKey(ctx, w.api.Pgcontainer.DB)
		require.NoError(t, err)
		require.Equal(t, key, stored.Key)
		require.Equal(t, w.adminId, husonymdb.UUIDString(stored.CreatedByUserID))
	})

	t.Run("a key issued before the one in force is a failed precondition giving the reason", func(t *testing.T) {
		_, err := w.set(ctx, w.admin, older)
		require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))
		require.Contains(t, err.Error(), "not after the key in force")
		require.Contains(t, err.Error(), now.Add(-2*time.Hour).Format(time.RFC3339))
		require.Contains(t, err.Error(), now.Add(-time.Hour).Format(time.RFC3339))
		require.NotContains(t, err.Error(), older)
		require.NotContains(t, err.Error(), key)

		stored, err := w.api.HusonymQuerier.GetCurrentLicenseKey(ctx, w.api.Pgcontainer.DB)
		require.NoError(t, err)
		require.Equal(t, key, stored.Key)
	})

	t.Run("the key in force given again changes nothing and is answered", func(t *testing.T) {
		resp, err := w.set(ctx, w.admin, key)
		require.NoError(t, err)
		require.Equal(t, "valid", resp.Msg.GetLicense().GetState())
	})

	// The accepted behaviour, stated as it is: the account of the request is the caller's
	// choice, and everyone administers their own personal account. Someone who only views
	// the team account therefore gets through with their own. What they can do with it is
	// what anyone can: put a key its issuer signed, newer than the one in force.
	t.Run("the viewer of the team sets the license through their own personal account", func(t *testing.T) {
		personalAccountId := tchusonymapi.CreatePersonalAccount(ctx, t, viewer)

		_, err := setThrough(ctx, viewer, personalAccountId, older)
		require.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(err))

		_, err = setThrough(ctx, viewer, personalAccountId, "not-a-key")
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

		// Signed by someone else than the issuer the instance trusts.
		_, foreignSigningKey, err := ed25519.GenerateKey(rand.Reader)
		require.NoError(t, err)
		forged, err := license.Issue(&license.IssueRequest{
			IssuedTo:   "Acme Co.",
			CustomerId: "cust-001",
			ExpiresAt:  time.Now().UTC().Add(90 * 24 * time.Hour),
		}, foreignSigningKey, license.Keyring{license.LegacyKid: foreignSigningKey.Public().(ed25519.PublicKey)})
		require.NoError(t, err)
		_, err = setThrough(ctx, viewer, personalAccountId, forged.Encoded)
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

		stored, err := w.api.HusonymQuerier.GetCurrentLicenseKey(ctx, w.api.Pgcontainer.DB)
		require.NoError(t, err)
		require.Equal(t, key, stored.Key, "none of the three replaced the key in force")

		newer := w.issue(t, now, nil)
		resp, err := setThrough(ctx, viewer, personalAccountId, newer)
		require.NoError(t, err)
		require.True(t, resp.Msg.GetLicense().GetIsValid())

		stored, err = w.api.HusonymQuerier.GetCurrentLicenseKey(ctx, w.api.Pgcontainer.DB)
		require.NoError(t, err)
		require.Equal(t, newer, stored.Key)
	})
}

func Test_GetSystemLicenseKey_IsForTheWorkerOnly(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	w := newLicenseWorld(ctx, t)
	worker := w.api.OSSAuthenticatedStoredLicenseClients.Users(tchusonymapi.WithUserId(apikey.NewV1WorkerKey()))
	read := func(client mgmtv1alpha1connect.UserAccountServiceClient) (string, error) {
		resp, err := client.GetSystemLicenseKey(ctx, connect.NewRequest(&mgmtv1alpha1.GetSystemLicenseKeyRequest{}))
		if err != nil {
			return "", err
		}
		return resp.Msg.GetKey(), nil
	}

	value, err := read(worker)
	require.NoError(t, err)
	require.Empty(t, value, "an instance without a key has none to give")

	key := w.issue(t, time.Now().UTC().Add(-time.Hour), nil)
	_, err = w.set(ctx, w.admin, key)
	require.NoError(t, err)

	// A person does not read the key, even the one who gave it.
	_, err = read(w.admin)
	require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))

	// The worker reads it as it was signed.
	value, err = read(worker)
	require.NoError(t, err)
	require.Equal(t, key, value)
}

func Test_GetSystemInformation_DescribesTheLicense(t *testing.T) {
	if !testutil.ShouldRunIntegrationTest() {
		return
	}
	ctx := t.Context()
	w := newLicenseWorld(ctx, t)
	now := time.Now().UTC().Truncate(time.Second)

	everyFeature := make([]string, 0, len(license.AllFeatures()))
	for _, feature := range license.AllFeatures() {
		everyFeature = append(everyFeature, string(feature))
	}
	require.Len(t, everyFeature, 13)

	t.Run("without a key, nothing of a key is described", func(t *testing.T) {
		described := w.described(ctx, t)
		require.Equal(t, "none", described.GetState())
		require.False(t, described.GetAllFeatures())
		require.Empty(t, described.GetFeatures())
		require.Empty(t, described.GetOrigin())
		require.Nil(t, described.GetInstalledAt())
		require.Nil(t, described.GetLimits())
		require.Nil(t, described.GetGraceEndsAt())
		require.Nil(t, described.Problem)
	})

	t.Run("a key that lists no feature, as the ones issued before the list, allows the thirteen", func(t *testing.T) {
		_, err := w.set(ctx, w.admin, w.issue(t, now.Add(-3*time.Hour), nil))
		require.NoError(t, err)

		described := w.described(ctx, t)
		require.True(t, described.GetIsValid())
		require.True(t, described.GetAllFeatures())
		require.Equal(t, everyFeature, described.GetFeatures())
		require.Equal(t, "online", described.GetTelemetry())
		require.Equal(t, "Acme Co.", described.GetIssuedTo())
		require.Empty(t, described.GetPlan())
		require.Nil(t, described.GetLimits())
	})

	t.Run("a key that names its features is described with exactly those", func(t *testing.T) {
		expiresAt := now.Add(60 * 24 * time.Hour)
		maxSources, graceDays := 5, 7
		_, err := w.set(ctx, w.admin, w.issue(t, now.Add(-2*time.Hour), func(req *license.IssueRequest) {
			req.ExpiresAt = expiresAt
			req.GraceDays = &graceDays
			req.Plan = "team"
			// Not in the order the product lists them: the description is.
			req.Features = []string{"mcp", "job_hooks"}
			req.Telemetry = "offline_report"
			req.Limits = &license.Limits{MaxSources: &maxSources, AllowedConnectionTypes: []string{"postgres"}}
		}))
		require.NoError(t, err)

		described := w.described(ctx, t)
		require.True(t, described.GetIsValid())
		require.Equal(t, "valid", described.GetState())
		require.False(t, described.GetAllFeatures())
		require.Equal(t, []string{"job_hooks", "mcp"}, described.GetFeatures())
		require.Equal(t, "team", described.GetPlan())
		require.Equal(t, "offline_report", described.GetTelemetry())
		require.True(t, expiresAt.Equal(described.GetExpiresAt().AsTime()))
		require.True(t, expiresAt.Add(7*24*time.Hour).Equal(described.GetGraceEndsAt().AsTime()))
		require.EqualValues(t, 5, described.GetLimits().GetMaxSources())
		require.Nil(t, described.GetLimits().MaxJobs)
		require.Nil(t, described.GetLimits().MaxConnections)
		require.Equal(t, []string{"postgres"}, described.GetLimits().GetAllowedConnectionTypes())
		require.Equal(t, "interface", described.GetOrigin())
		require.WithinDuration(t, time.Now(), described.GetInstalledAt().AsTime(), time.Minute)
		require.Nil(t, described.Problem)
	})

	t.Run("a key that names the wildcard allows every feature", func(t *testing.T) {
		_, err := w.set(ctx, w.admin, w.issue(t, now.Add(-time.Hour), func(req *license.IssueRequest) {
			req.Features = []string{license.FeatureWildcard}
		}))
		require.NoError(t, err)

		described := w.described(ctx, t)
		require.True(t, described.GetAllFeatures())
		require.Equal(t, everyFeature, described.GetFeatures())
	})
}
