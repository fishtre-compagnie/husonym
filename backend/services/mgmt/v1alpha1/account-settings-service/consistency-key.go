package v1alpha1_accountsettingservice

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"

	"connectrpc.com/connect"
	db_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/internal/ee/rbac"
	husonymerrors "github.com/fishtre-compagnie/husonym/internal/errors"
	"github.com/fishtre-compagnie/husonym/internal/husonymdb"
	"github.com/jackc/pgx/v5/pgtype"
)

// derivationKeyBytes is the size of a generated key. Anyone who knows the key can recover
// low-entropy values — phone numbers, first names — by trying them all, so the key itself
// must never be the weak part.
const derivationKeyBytes = 32

// GetAccountConsistencyKey returns the key the account's deterministic anonymization
// derives from, in clear, and generates one when asked and the account has none.
//
// Only a run reads it: it already holds the passwords of the databases it synchronizes, so
// the key of the account it runs for adds no surface. The same lock ReconcileJobMappings
// applies keeps anyone else out.
func (s *Service) GetAccountConsistencyKey(
	ctx context.Context,
	req *connect.Request[mgmtv1alpha1.GetAccountConsistencyKeyRequest],
) (*connect.Response[mgmtv1alpha1.GetAccountConsistencyKeyResponse], error) {
	user, accountUuid, err := s.enforce(ctx, req.Msg.GetAccountId(), rbac.AccountAction_View)
	if err != nil {
		return nil, err
	}
	if s.cfg.IsHusonymCloud && !user.IsWorkerApiKey() {
		return nil, husonymerrors.NewUnauthenticated(
			"must provide valid authentication credentials for this endpoint",
		)
	}

	key, err := s.consistencyKey(ctx, accountUuid)
	if err != nil {
		return nil, err
	}
	if key == "" && req.Msg.GetGenerateIfAbsent() {
		key, err = s.generateConsistencyKey(ctx, accountUuid)
		if err != nil {
			return nil, err
		}
	}

	resp := &mgmtv1alpha1.GetAccountConsistencyKeyResponse{}
	if key != "" {
		resp.Key = &key
	}
	return connect.NewResponse(resp), nil
}

// consistencyKey reads the key the account holds, or nothing when it holds none.
func (s *Service) consistencyKey(ctx context.Context, accountUuid pgtype.UUID) (string, error) {
	row, err := s.db.Q.GetAccountSettingByType(ctx, s.db.Db, db_queries.GetAccountSettingByTypeParams{
		AccountID:   accountUuid,
		SettingType: pgtype.Text{String: settingTypeAnonymizationConsistency, Valid: true},
	})
	if husonymdb.IsNoRows(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	config, err := s.storedConfig(row.Config)
	if err != nil {
		return "", err
	}
	return config.GetAnonymizationConsistency().GetDerivationKey(), nil
}

// generateConsistencyKey gives the account a key of its own.
//
// Two runs of the same account starting together each draw one; the row that lands first
// is the one both then read, because the outputs of a run must derive from a single key.
// No user is recorded: nobody chose this key.
func (s *Service) generateConsistencyKey(
	ctx context.Context,
	accountUuid pgtype.UUID,
) (string, error) {
	key, err := newDerivationKey()
	if err != nil {
		return "", err
	}
	config, err := s.encryptedConfig(&mgmtv1alpha1.AccountSettingConfig{
		Config: &mgmtv1alpha1.AccountSettingConfig_AnonymizationConsistency{
			AnonymizationConsistency: &mgmtv1alpha1.AnonymizationConsistency{DerivationKey: key},
		},
	})
	if err != nil {
		return "", err
	}

	_, err = s.db.Q.CreateAccountSettingIfAbsent(ctx, s.db.Db, db_queries.CreateAccountSettingIfAbsentParams{
		AccountID:       accountUuid,
		Config:          config,
		CreatedByUserID: pgtype.UUID{},
	})
	if husonymdb.IsNoRows(err) {
		// Somebody got there first: theirs is the key of the account.
		return s.consistencyKey(ctx, accountUuid)
	}
	if err != nil {
		return "", err
	}
	return key, nil
}

func newDerivationKey() (string, error) {
	key := make([]byte, derivationKeyBytes)
	if _, err := rand.Read(key); err != nil {
		return "", fmt.Errorf("unable to draw a consistency key: %w", err)
	}
	return base64.StdEncoding.EncodeToString(key), nil
}
