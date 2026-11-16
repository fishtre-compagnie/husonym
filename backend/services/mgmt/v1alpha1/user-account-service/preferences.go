package v1alpha1_useraccountservice

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/jackc/pgx/v5/pgtype"
)

// supportedLocales are the languages of the interface, the empty one standing for the default
// of the instance. They are the list the contract and the check constraint of the column hold.
var supportedLocales = map[string]struct{}{"": {}, "en": {}, "fr": {}}

// SetUserPreferences stores the preferences of the person calling. It touches no account, so
// it asks no permission of the RBAC; an API key, which has no person behind it, is refused,
// and so is a token issued to an application. With authentication off, the preferences are
// the anonymous user's, which every visitor of the instance shares.
func (s *Service) SetUserPreferences(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.SetUserPreferencesRequest],
) (*connect.Response[mgmtv1alpha1.SetUserPreferencesResponse], error) {
	// The contract validates the locale already; this holds where the validation interceptor
	// is not wired, and keeps the check constraint of the column from answering as an internal
	// error.
	if _, ok := supportedLocales[req.Msg.GetLocale()]; !ok {
		return nil, husonymerrors.NewBadRequest(fmt.Sprintf("unsupported locale %q", req.Msg.GetLocale()))
	}
	who, err := s.resolveCaller(ctx)
	if err != nil {
		return nil, err
	}
	if !who.person {
		return nil, husonymerrors.NewForbidden(
			"preferences belong to a person, and this caller is an API key or an application",
		)
	}
	userId, err := husonymdb.ToUuid(who.userId)
	if err != nil {
		return nil, husonymerrors.New(err)
	}

	// The empty locale is stored as null: the user follows the default of the instance.
	stored, err := s.db.Q.SetUserLocale(ctx, s.db.Db, db_queries.SetUserLocaleParams{
		Locale: pgtype.Text{String: req.Msg.GetLocale(), Valid: req.Msg.GetLocale() != ""},
		UserId: userId,
	})
	if err != nil && husonymdb.IsNoRows(err) {
		return nil, husonymerrors.NewNotFound("unable to find user")
	} else if err != nil {
		return nil, husonymerrors.New(err)
	}
	return connect.NewResponse(&mgmtv1alpha1.SetUserPreferencesResponse{
		Locale: stored.String,
	}), nil
}

// userLocale reads the language a user chose for the interface, empty when the user follows
// the default of the instance.
func (s *Service) userLocale(ctx context.Context, userId string) (string, error) {
	id, err := husonymdb.ToUuid(userId)
	if err != nil {
		return "", husonymerrors.New(err)
	}
	user, err := s.db.Q.GetUser(ctx, s.db.Db, id)
	if err != nil && husonymdb.IsNoRows(err) {
		return "", husonymerrors.NewNotFound("unable to find user")
	} else if err != nil {
		return "", husonymerrors.New(err)
	}
	return user.Locale.String, nil
}
