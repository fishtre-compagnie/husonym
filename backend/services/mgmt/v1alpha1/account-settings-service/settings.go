package v1alpha1_accountsettingservice

import (
	"context"
	"encoding/json"
	"fmt"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/backend/internal/userdata"
	"github.com/fishtre-compagnie/husonym/internal/ee/rbac"
	"github.com/fishtre-compagnie/husonym/internal/encrypt/protosecret"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// settingTypeAnonymizationConsistency is what the generated column of account_settings
// derives from a config holding that variant. The migration and this name are held in step
// by TestSettingTypesMatchTheMigration.
const settingTypeAnonymizationConsistency = "anonymization_consistency"

// GetAccountSettings returns the settings of an account, each without its secrets: a
// fingerprint stands in their place (§8.3 of the plan). Reading a setting is reading the
// account, so viewing the account is enough.
func (s *Service) GetAccountSettings(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetAccountSettingsRequest],
) (*connect.Response[mgmtv1alpha1.GetAccountSettingsResponse], error) {
	_, accountUuid, err := s.enforce(ctx, req.Msg.GetAccountId(), rbac.AccountAction_View)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.Q.GetAccountSettings(ctx, s.db.Db, accountUuid)
	if err != nil {
		return nil, err
	}

	settings := make([]*mgmtv1alpha1.AccountSetting, 0, len(rows))
	for idx := range rows {
		setting, err := s.toRedactedDto(&rows[idx])
		if err != nil {
			return nil, err
		}
		settings = append(settings, setting)
	}

	return connect.NewResponse(&mgmtv1alpha1.GetAccountSettingsResponse{
		Settings: settings,
	}), nil
}

// SetAccountSetting writes a setting, replacing the one the account holds of that kind.
//
// Editing the account is what it takes: the settings are the account's, and replacing one
// of them — the consistency key above all — changes what every run of the account then
// produces.
func (s *Service) SetAccountSetting(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.SetAccountSettingRequest],
) (*connect.Response[mgmtv1alpha1.SetAccountSettingResponse], error) {
	user, accountUuid, err := s.enforce(ctx, req.Msg.GetAccountId(), rbac.AccountAction_Edit)
	if err != nil {
		return nil, err
	}

	config, err := s.encryptedConfig(req.Msg.GetConfig())
	if err != nil {
		return nil, err
	}

	row, err := s.db.Q.UpsertAccountSetting(ctx, s.db.Db, db_queries.UpsertAccountSettingParams{
		AccountID:       accountUuid,
		Config:          config,
		CreatedByUserID: user.PgId(),
	})
	if err != nil {
		return nil, err
	}

	setting, err := s.toRedactedDto(&row)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&mgmtv1alpha1.SetAccountSettingResponse{Setting: setting}), nil
}

// enforce checks that the caller may act on the account, and hands back the caller and the
// id of the account for the database.
func (s *Service) enforce(
	ctx context.Context,
	accountId string,
	action rbac.AccountAction,
) (*userdata.User, pgtype.UUID, error) {
	user, err := s.userdataclient.GetUser(ctx)
	if err != nil {
		return nil, pgtype.UUID{}, err
	}
	if err := user.EnforceAccount(ctx, userdata.NewIdentifier(accountId), action); err != nil {
		return nil, pgtype.UUID{}, err
	}
	accountUuid, err := husonymdb.ToUuid(accountId)
	if err != nil {
		return nil, pgtype.UUID{}, err
	}
	return user, accountUuid, nil
}

// encryptedConfig turns a config into what the column holds: its secret fields encrypted
// one by one, the rest as it stands, serialized the way the generated column reads it.
func (s *Service) encryptedConfig(config *mgmtv1alpha1.AccountSettingConfig) ([]byte, error) {
	encrypted, err := protosecret.Encrypt(s.encryptor, config)
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(encrypted)
	if err != nil {
		return nil, fmt.Errorf("unable to serialize an account setting: %w", err)
	}
	return out, nil
}

// storedConfig reads back what encryptedConfig wrote, with its secrets in clear.
func (s *Service) storedConfig(stored []byte) (*mgmtv1alpha1.AccountSettingConfig, error) {
	config := &mgmtv1alpha1.AccountSettingConfig{}
	if err := json.Unmarshal(stored, config); err != nil {
		return nil, fmt.Errorf("unable to read an account setting: %w", err)
	}
	plain, err := protosecret.Decrypt(s.encryptor, config)
	if err != nil {
		// There is one key, with no version and no fallback, so a password that is not the
		// one that wrote this setting cannot read it. Say which password, rather than let a
		// rotation look like a corrupt row.
		return nil, fmt.Errorf(
			"unable to read an account setting: HUSONYM_SYM_ENCRYPTION_PASSWORD is not the "+
				"one this setting was written with: %w", err,
		)
	}
	return plain, nil
}

// toRedactedDto is what leaves towards a user: the setting without its secrets, and the
// fingerprint of each one it holds.
func (s *Service) toRedactedDto(
	row *db_queries.HusonymApiAccountSetting,
) (*mgmtv1alpha1.AccountSetting, error) {
	config, err := s.storedConfig(row.Config)
	if err != nil {
		return nil, err
	}
	redacted, fingerprints, err := protosecret.Redact(config)
	if err != nil {
		return nil, err
	}
	return &mgmtv1alpha1.AccountSetting{
		AccountId:          husonymdb.UUIDString(row.AccountID),
		Config:             redacted,
		SecretFingerprints: fingerprints,
		CreatedAt:          timestamppb.New(row.CreatedAt.Time),
		UpdatedAt:          timestamppb.New(row.UpdatedAt.Time),
		CreatedByUserId:    optionalUUIDString(row.CreatedByUserID),
		UpdatedByUserId:    optionalUUIDString(row.UpdatedByUserID),
	}, nil
}

// optionalUUIDString leaves the field empty rather than writing a zero uuid: a setting a
// run generated has no user behind it.
func optionalUUIDString(value pgtype.UUID) string {
	if !value.Valid {
		return ""
	}
	return husonymdb.UUIDString(value)
}
