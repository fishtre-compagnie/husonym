package v1alpha1_accountsettingservice

import (
	"context"

	"connectrpc.com/connect"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/internal/oidcprobe"
	"github.com/fishtre-compagnie/husonym/internal/ee/rbac"
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
		checks := oidcprobe.New().Run(ctx, oidcprobe.Input{
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
