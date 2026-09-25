package v1alpha1_accountsettingservice

import (
	"context"
	"fmt"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/internal/oidcprobe"
	"github.com/fishtre-compagnie/husonym/internal/ee/rbac"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/jackc/pgx/v5/pgtype"
)

// TestAccountSetting tries a setting without writing it.
//
// It takes the same right as writing one, and for a reason that is not symmetry: trying a
// setting makes this deployment fetch a URL the caller chose. Whoever may cause that must
// be whoever may configure it -- otherwise the endpoint is a way for any member to have
// the server make requests on their behalf.
func (s *Service) TestAccountSetting(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.TestAccountSettingRequest],
) (*connect.Response[mgmtv1alpha1.TestAccountSettingResponse], error) {
	if _, _, err := s.enforce(ctx, req.Msg.GetAccountId(), rbac.AccountAction_Edit); err != nil {
		return nil, err
	}

	switch config := req.Msg.GetConfig().GetConfig().(type) {
	case *mgmtv1alpha1.AccountSettingConfig_OidcProvider:
		checks := oidcprobe.New(s.cfg.IssuerPolicy).Run(ctx, oidcprobe.Input{
			Issuer:             config.OidcProvider.GetIssuer(),
			ClientID:           config.OidcProvider.GetClientId(),
			AcceptedAlgorithms: s.cfg.AcceptedSignatureAlgorithms,
		})
		return connect.NewResponse(&mgmtv1alpha1.TestAccountSettingResponse{
			Checks: toCheckDtos(checks),
			Ok:     !oidcprobe.HasBlocking(checks),
		}), nil
	default:
		// Not an error for the caller to have to handle: a setting with nothing to try is
		// a setting that passes.
		return connect.NewResponse(&mgmtv1alpha1.TestAccountSettingResponse{
			Checks: []*mgmtv1alpha1.SettingCheck{},
			Ok:     true,
		}), nil
	}
}

func toCheckDtos(checks []oidcprobe.Check) []*mgmtv1alpha1.SettingCheck {
	dtos := make([]*mgmtv1alpha1.SettingCheck, 0, len(checks))
	for _, check := range checks {
		dtos = append(dtos, &mgmtv1alpha1.SettingCheck{
			Check:  check.Check,
			Level:  toCheckLevelDto(check.Level),
			Detail: check.Detail,
			Remedy: check.Remedy,
		})
	}
	return dtos
}

func toCheckLevelDto(level oidcprobe.Level) mgmtv1alpha1.SettingCheckLevel {
	switch level {
	case oidcprobe.LevelBlocking:
		return mgmtv1alpha1.SettingCheckLevel_SETTING_CHECK_LEVEL_BLOCKING
	case oidcprobe.LevelWarning:
		return mgmtv1alpha1.SettingCheckLevel_SETTING_CHECK_LEVEL_WARNING
	case oidcprobe.LevelInfo:
		return mgmtv1alpha1.SettingCheckLevel_SETTING_CHECK_LEVEL_INFO
	}
	return mgmtv1alpha1.SettingCheckLevel_SETTING_CHECK_LEVEL_UNSPECIFIED
}

// refuseIssuerClaimedElsewhere stops an account from declaring a provider another account
// has already declared.
//
// Identities are keyed by (issuer, subject), which means two accounts trusting the same
// issuer share the subject space it mints. Whoever administers sign-ins at that provider
// could then hand themselves the identity of a member of the other account -- the very
// thing keying on the issuer is there to prevent, reintroduced one level up.
//
// It is a refusal and not a finding, because unlike everything the probe reports this one
// cannot be judged by whoever is configuring: the other account is none of their business,
// and the message says nothing about it beyond that it exists.
func (s *Service) refuseIssuerClaimedElsewhere(
	ctx context.Context,
	accountUuid pgtype.UUID,
	config *mgmtv1alpha1.AccountSettingConfig,
) error {
	provider, ok := config.GetConfig().(*mgmtv1alpha1.AccountSettingConfig_OidcProvider)
	if !ok {
		return nil
	}
	issuer := provider.OidcProvider.GetIssuer()
	if issuer == "" {
		return nil
	}

	claimed, err := s.db.Q.CountOtherAccountsDeclaringIssuer(
		ctx,
		s.db.Db,
		db_queries.CountOtherAccountsDeclaringIssuerParams{Issuer: issuer, AccountId: accountUuid},
	)
	if err != nil {
		return err
	}
	if claimed > 0 {
		return husonymerrors.NewBadRequest(
			"this identity provider is already declared by another account. Two accounts cannot share one, because they would share the identities it issues",
		)
	}
	return nil
}

// refuseIssuerOutOfReach stops an account from declaring a provider this deployment must
// not be made to contact: one not on https, or whose host resolves inside the deployment's
// network -- the deployment itself, its neighbors, or the cloud's metadata service. The
// deployment fetches what that provider publishes, so the account would otherwise choose
// where it sends requests. Trying the setting reports the same; saving it without trying
// must not get around it.
func (s *Service) refuseIssuerOutOfReach(
	ctx context.Context,
	config *mgmtv1alpha1.AccountSettingConfig,
) error {
	provider, ok := config.GetConfig().(*mgmtv1alpha1.AccountSettingConfig_OidcProvider)
	if !ok {
		return nil
	}
	if err := s.cfg.IssuerPolicy.CheckIssuer(ctx, provider.OidcProvider.GetIssuer()); err != nil {
		return husonymerrors.NewBadRequest(fmt.Sprintf(
			"this identity provider cannot be used: %s. It must be reached over https, at an address on the internet",
			err,
		))
	}
	return nil
}
