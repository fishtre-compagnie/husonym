-- The organization of the instance is the one account the instance retains as its own.

-- Holds the row of the instance for the rest of the transaction, so that what is decided once
-- per instance is decided by one transaction at a time: a second one waits here until the first
-- is done.
-- name: LockInstance :one
SELECT id
FROM husonym_api.instance
FOR UPDATE;

-- Null while no organization is retained.
-- name: GetInstanceOrganization :one
SELECT organization_account_id
FROM husonym_api.instance;

-- The accounts that have a person among their members. A person is a user an identity provider
-- vouches for: neither the anonymous user nor the user of an API key is one, so the account of
-- either alone is not counted. A person who is in no account yet counts for nothing.
-- name: CountAccountsWithPersonMember :one
SELECT count(DISTINCT aua.account_id)::bigint
FROM husonym_api.account_user_associations aua
WHERE EXISTS (
  SELECT 1
  FROM husonym_api.user_identity_provider_associations uipa
  WHERE uipa.user_id = aua.user_id
);

-- Only an instance that retains none retains one: the first stays. The count of rows tells
-- whether this call retained it.
-- name: SetInstanceOrganization :execrows
UPDATE husonym_api.instance
SET organization_account_id = sqlc.arg('accountId')
WHERE organization_account_id IS NULL;

-- Creates a team account under an id chosen by the caller, who needed the id before the account
-- existed.
-- name: CreateTeamAccountWithId :one
INSERT INTO husonym_api.accounts (
  id, account_type, account_slug
) VALUES (
  sqlc.arg('id'), 1, sqlc.arg('accountSlug')
)
RETURNING *;
