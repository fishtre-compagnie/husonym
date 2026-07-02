-- name: GetAccountApiKeyById :one
SELECT * from husonym_api.account_api_keys WHERE id = $1;

-- name: GetAccountApiKeys :many
SELECT aak.* from husonym_api.account_api_keys aak
INNER JOIN husonym_api.accounts a on a.id = aak.account_id
WHERE a.id = sqlc.arg('accountId');

-- name: RemoveAccountApiKey :exec
DELETE FROM husonym_api.account_api_keys WHERE id = $1;

-- name: GetAccountApiKeyByKeyValue :one
SELECT * from husonym_api.account_api_keys WHERE key_value = $1;

-- name: CreateAccountApiKey :one
INSERT INTO husonym_api.account_api_keys (
  key_name, key_value, account_id, expires_at, created_by_id, updated_by_id, user_id
) VALUES (
  $1, $2, $3, $4, $5, $6, $7
)
RETURNING *;

-- name: UpdateAccountApiKeyValue :one
UPDATE husonym_api.account_api_keys
SET key_value = $1,
    expires_at = $2,
    updated_by_id = $3
WHERE id = $4
RETURNING *;


-- name: IsUserInAccountApiKey :one
SELECT count(apk.id) from husonym_api.account_api_keys apk 
INNER JOIN husonym_api.accounts a ON a.id = apk.account_id
INNER JOIN husonym_api.users u ON u.id = apk.user_id
WHERE a.id = sqlc.arg('accountId') AND u.id = sqlc.arg('userId');
