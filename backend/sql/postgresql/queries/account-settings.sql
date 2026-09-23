-- name: GetAccountSettings :many
SELECT * FROM husonym_api.account_settings
WHERE account_id = $1
ORDER BY setting_type ASC;

-- name: GetAccountSettingByType :one
SELECT * FROM husonym_api.account_settings
WHERE account_id = $1 AND setting_type = $2;

-- name: UpsertAccountSetting :one
-- The kind of setting is the generated column, so the conflict is named by its constraint
-- rather than by the columns it covers.
INSERT INTO husonym_api.account_settings (
  account_id, config, created_by_user_id, updated_by_user_id
) VALUES (
  $1, $2, $3, $3
)
ON CONFLICT ON CONSTRAINT account_settings_one_per_type DO UPDATE
SET config = EXCLUDED.config,
    updated_by_user_id = EXCLUDED.updated_by_user_id,
    updated_at = CURRENT_TIMESTAMP
RETURNING *;

-- name: CreateAccountSettingIfAbsent :one
-- Returns no row when the account already has a setting of that kind: the caller reads the
-- one that is there. Two runs of the same account that both generate a key that way keep
-- the one that won.
INSERT INTO husonym_api.account_settings (
  account_id, config, created_by_user_id, updated_by_user_id
) VALUES (
  $1, $2, $3, $3
)
ON CONFLICT ON CONSTRAINT account_settings_one_per_type DO NOTHING
RETURNING *;
