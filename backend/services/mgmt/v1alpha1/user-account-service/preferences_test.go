package v1alpha1_useraccountservice

import (
	"context"
	"errors"
	"testing"

	"buf.build/go/protovalidate"
	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	auth_apikey "github.com/fishtre-compagnie/husonym/backend/internal/auth/apikey"
	authjwt "github.com/fishtre-compagnie/husonym/backend/internal/auth/jwt"
	"github.com/fishtre-compagnie/husonym/internal/apikey"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	aPersonId  = "6f1c2b8e-3a4d-4e5f-9a0b-1c2d3e4f5a6b"
	aMachineId = "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d"
	anonymous  = "00000000-0000-0000-0000-000000000000"
)

type prefsFixture struct {
	svc     *Service
	querier *db_queries.MockQuerier
}

func newPrefsFixture(t *testing.T, authEnabled bool) *prefsFixture {
	t.Helper()
	querier := db_queries.NewMockQuerier(t)
	return &prefsFixture{
		svc: &Service{
			cfg: &Config{IsAuthEnabled: authEnabled},
			db:  husonymdb.New(husonymdb.NewMockDBTX(t), querier),
		},
		querier: querier,
	}
}

func mustUuid(t *testing.T, value string) pgtype.UUID {
	t.Helper()
	id, err := husonymdb.ToUuid(value)
	require.NoError(t, err)
	return id
}

// personCtx is a person signed in through the provider, whose identity maps to aPersonId.
func (f *prefsFixture) personCtx(t *testing.T, claims *authjwt.CustomClaims) context.Context {
	t.Helper()
	f.querier.EXPECT().
		GetUserAssociationByIdentity(mock.Anything, mock.Anything, db_queries.GetUserAssociationByIdentityParams{
			ProviderSub: "sub-1", ProviderIss: "https://idp.example.com",
		}).
		Return(db_queries.HusonymApiUserIdentityProviderAssociation{
			UserID: mustUuid(t, aPersonId), ProviderSub: "sub-1", ProviderIss: "https://idp.example.com",
		}, nil)
	return authjwt.SetTokenData(context.Background(), &authjwt.TokenContextData{
		AuthUserId: "sub-1",
		AuthIssuer: "https://idp.example.com",
		Claims:     claims,
	})
}

func accountKeyCtx(t *testing.T) context.Context {
	t.Helper()
	return auth_apikey.SetTokenData(context.Background(), &auth_apikey.TokenContextData{
		ApiKeyType: apikey.AccountApiKey,
		ApiKey:     &db_queries.HusonymApiAccountApiKey{UserID: mustUuid(t, aMachineId)},
	})
}

func workerKeyCtx() context.Context {
	return auth_apikey.SetTokenData(context.Background(), &auth_apikey.TokenContextData{ApiKeyType: apikey.WorkerApiKey})
}

func setLocale(locale string) *connect.Request[mgmtv1alpha1.SetUserPreferencesRequest] {
	return connect.NewRequest(&mgmtv1alpha1.SetUserPreferencesRequest{Locale: locale})
}

func Test_SetUserPreferencesRequest_Validation(t *testing.T) {
	validator, err := protovalidate.New()
	require.NoError(t, err)

	for _, locale := range []string{"", "en", "fr"} {
		t.Run("accepts "+locale, func(t *testing.T) {
			require.NoError(t, validator.Validate(&mgmtv1alpha1.SetUserPreferencesRequest{Locale: locale}))
		})
	}
	for _, locale := range []string{"de", "EN", "fr-FR", "en_US", " fr", "xx"} {
		t.Run("refuses "+locale, func(t *testing.T) {
			require.Error(t, validator.Validate(&mgmtv1alpha1.SetUserPreferencesRequest{Locale: locale}))
		})
	}
}

func Test_SetUserPreferences(t *testing.T) {
	t.Run("a person stores a locale", func(t *testing.T) {
		f := newPrefsFixture(t, true)
		ctx := f.personCtx(t, &authjwt.CustomClaims{})
		f.querier.EXPECT().
			SetUserLocale(mock.Anything, mock.Anything, db_queries.SetUserLocaleParams{
				Locale: pgtype.Text{String: "fr", Valid: true},
				UserId: mustUuid(t, aPersonId),
			}).
			Return(pgtype.Text{String: "fr", Valid: true}, nil)

		resp, err := f.svc.SetUserPreferences(ctx, setLocale("fr"))
		require.NoError(t, err)
		require.Equal(t, "fr", resp.Msg.GetLocale())
	})

	t.Run("the empty locale is stored as null and read back empty", func(t *testing.T) {
		f := newPrefsFixture(t, true)
		ctx := f.personCtx(t, nil)
		f.querier.EXPECT().
			SetUserLocale(mock.Anything, mock.Anything, db_queries.SetUserLocaleParams{
				Locale: pgtype.Text{},
				UserId: mustUuid(t, aPersonId),
			}).
			Return(pgtype.Text{}, nil)

		resp, err := f.svc.SetUserPreferences(ctx, setLocale(""))
		require.NoError(t, err)
		require.Empty(t, resp.Msg.GetLocale())
	})

	t.Run("with authentication off, the anonymous user stores it", func(t *testing.T) {
		f := newPrefsFixture(t, false)
		f.querier.EXPECT().GetAnonymousUser(mock.Anything, mock.Anything).
			Return(db_queries.HusonymApiUser{ID: mustUuid(t, anonymous)}, nil)
		f.querier.EXPECT().
			SetUserLocale(mock.Anything, mock.Anything, db_queries.SetUserLocaleParams{
				Locale: pgtype.Text{String: "en", Valid: true},
				UserId: mustUuid(t, anonymous),
			}).
			Return(pgtype.Text{String: "en", Valid: true}, nil)

		resp, err := f.svc.SetUserPreferences(context.Background(), setLocale("en"))
		require.NoError(t, err)
		require.Equal(t, "en", resp.Msg.GetLocale())
	})

	refused := map[string]func(t *testing.T, f *prefsFixture) context.Context{
		"an account key": func(t *testing.T, _ *prefsFixture) context.Context { return accountKeyCtx(t) },
		"the worker key": func(*testing.T, *prefsFixture) context.Context { return workerKeyCtx() },
		"an application token": func(t *testing.T, f *prefsFixture) context.Context {
			return f.personCtx(t, &authjwt.CustomClaims{IdentityType: "app"})
		},
	}
	for name, ctxOf := range refused {
		t.Run(name+" is refused and nothing is written", func(t *testing.T) {
			f := newPrefsFixture(t, true)
			_, err := f.svc.SetUserPreferences(ctxOf(t, f), setLocale("fr"))
			require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
			f.querier.AssertNotCalled(t, "SetUserLocale", mock.Anything, mock.Anything, mock.Anything)
		})
	}

	t.Run("an account key is refused with authentication off too", func(t *testing.T) {
		f := newPrefsFixture(t, false)
		_, err := f.svc.SetUserPreferences(accountKeyCtx(t), setLocale("fr"))
		require.Equal(t, connect.CodePermissionDenied, connect.CodeOf(err))
	})

	t.Run("an unsupported locale is refused before anything is read or written", func(t *testing.T) {
		f := newPrefsFixture(t, true)
		_, err := f.svc.SetUserPreferences(context.Background(), setLocale("de"))
		require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	})

	t.Run("a user that vanished is not found", func(t *testing.T) {
		f := newPrefsFixture(t, true)
		ctx := f.personCtx(t, nil)
		f.querier.EXPECT().SetUserLocale(mock.Anything, mock.Anything, mock.Anything).
			Return(pgtype.Text{}, pgx.ErrNoRows)

		_, err := f.svc.SetUserPreferences(ctx, setLocale("fr"))
		require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
	})

	t.Run("a database failure is an error", func(t *testing.T) {
		f := newPrefsFixture(t, true)
		ctx := f.personCtx(t, nil)
		f.querier.EXPECT().SetUserLocale(mock.Anything, mock.Anything, mock.Anything).
			Return(pgtype.Text{}, errors.New("the database is down"))

		_, err := f.svc.SetUserPreferences(ctx, setLocale("fr"))
		require.Error(t, err)
	})
}

func Test_GetUser_Locale(t *testing.T) {
	t.Run("a person reads the locale they chose", func(t *testing.T) {
		f := newPrefsFixture(t, true)
		ctx := f.personCtx(t, nil)
		f.querier.EXPECT().GetUser(mock.Anything, mock.Anything, mustUuid(t, aPersonId)).
			Return(db_queries.HusonymApiUser{ID: mustUuid(t, aPersonId), Locale: pgtype.Text{String: "fr", Valid: true}}, nil)

		resp, err := f.svc.GetUser(ctx, connect.NewRequest(&mgmtv1alpha1.GetUserRequest{}))
		require.NoError(t, err)
		require.Equal(t, aPersonId, resp.Msg.GetUserId())
		require.Equal(t, "fr", resp.Msg.GetLocale())
	})

	t.Run("a person who never chose reads it empty", func(t *testing.T) {
		f := newPrefsFixture(t, true)
		ctx := f.personCtx(t, nil)
		f.querier.EXPECT().GetUser(mock.Anything, mock.Anything, mustUuid(t, aPersonId)).
			Return(db_queries.HusonymApiUser{ID: mustUuid(t, aPersonId)}, nil)

		resp, err := f.svc.GetUser(ctx, connect.NewRequest(&mgmtv1alpha1.GetUserRequest{}))
		require.NoError(t, err)
		require.Empty(t, resp.Msg.GetLocale())
	})

	t.Run("with authentication off, the anonymous user's locale", func(t *testing.T) {
		f := newPrefsFixture(t, false)
		f.querier.EXPECT().GetAnonymousUser(mock.Anything, mock.Anything).
			Return(db_queries.HusonymApiUser{ID: mustUuid(t, anonymous)}, nil)
		f.querier.EXPECT().GetUser(mock.Anything, mock.Anything, mustUuid(t, anonymous)).
			Return(db_queries.HusonymApiUser{ID: mustUuid(t, anonymous), Locale: pgtype.Text{String: "en", Valid: true}}, nil)

		resp, err := f.svc.GetUser(context.Background(), connect.NewRequest(&mgmtv1alpha1.GetUserRequest{}))
		require.NoError(t, err)
		require.Equal(t, anonymous, resp.Msg.GetUserId())
		require.Equal(t, "en", resp.Msg.GetLocale())
	})

	t.Run("an account key gets its user and no locale, without reading the row", func(t *testing.T) {
		f := newPrefsFixture(t, true)
		resp, err := f.svc.GetUser(accountKeyCtx(t), connect.NewRequest(&mgmtv1alpha1.GetUserRequest{}))
		require.NoError(t, err)
		require.Equal(t, aMachineId, resp.Msg.GetUserId())
		require.Empty(t, resp.Msg.GetLocale())
	})

	t.Run("the worker key gets the zero user and no locale", func(t *testing.T) {
		f := newPrefsFixture(t, true)
		resp, err := f.svc.GetUser(workerKeyCtx(), connect.NewRequest(&mgmtv1alpha1.GetUserRequest{}))
		require.NoError(t, err)
		require.Equal(t, anonymous, resp.Msg.GetUserId())
		require.Empty(t, resp.Msg.GetLocale())
	})

	t.Run("the worker key with authentication off is not taken for the anonymous person", func(t *testing.T) {
		f := newPrefsFixture(t, false)
		resp, err := f.svc.GetUser(workerKeyCtx(), connect.NewRequest(&mgmtv1alpha1.GetUserRequest{}))
		require.NoError(t, err)
		require.Equal(t, anonymous, resp.Msg.GetUserId())
		require.Empty(t, resp.Msg.GetLocale())
	})
}
