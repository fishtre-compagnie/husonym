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

-- Every issuer an account has declared, for the resolver the token validator calls.
--
-- The issuer is read straight out of the jsonb and never decrypted, because it is not a
-- secret: it is the name a provider calls itself by, and it travels in every token. Only
-- the client secret of that setting is encrypted, and nothing here touches it.
--
-- Distinct, because two accounts pointing at the same provider is a list of one issuer,
-- not two -- the list says which tokens are authentic, never which account they open.
-- name: GetDeclaredIssuers :many
SELECT DISTINCT (config->'oidcProvider'->>'issuer')::text AS issuer
FROM husonym_api.account_settings
WHERE setting_type = 'oidc_provider'
  AND config->'oidcProvider'->>'issuer' IS NOT NULL
  AND config->'oidcProvider'->>'issuer' <> '';

-- Whether an issuer is declared by an account other than the one given. Two accounts
-- sharing an issuer share the subject space it mints, so the second one to claim it would
-- be able to name the members of the first.
-- name: CountOtherAccountsDeclaringIssuer :one
SELECT count(*)
FROM husonym_api.account_settings
WHERE setting_type = 'oidc_provider'
  AND config->'oidcProvider'->>'issuer' = sqlc.arg('issuer')
  AND account_id <> sqlc.arg('accountId');

-- The provider an account has declared, without its secrets being decrypted. The caller
-- reads the issuer, the client id and the audiences; the client secret stays as stored.
-- name: GetAccountOidcProvider :one
SELECT (config->'oidcProvider')::jsonb AS provider
FROM husonym_api.account_settings
WHERE account_id = sqlc.arg('accountId') AND setting_type = 'oidc_provider';

-- What an unauthenticated caller may learn about an account's provider, to start a sign-in.
--
-- Two columns, named one by one, and that is the point: the row also holds the client
-- secret, and this path serves anybody who can guess a slug. Selecting the whole config
-- and picking fields in Go would put the secret one careless line away from a response.
-- Here it never leaves the database.
-- name: GetAccountLoginMethodBySlug :one
SELECT
  (s.config->'oidcProvider'->>'issuer')::text AS issuer,
  (s.config->'oidcProvider'->>'clientId')::text AS client_id
FROM husonym_api.account_settings s
INNER JOIN husonym_api.accounts a ON a.id = s.account_id
WHERE a.account_slug = sqlc.arg('accountSlug') AND s.setting_type = 'oidc_provider';
