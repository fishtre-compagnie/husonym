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

-- Only an instance that retains none retains one: the first stays. The count of rows tells
-- whether this call retained it.
-- name: SetInstanceOrganization :execrows
UPDATE husonym_api.instance
SET organization_account_id = sqlc.arg('accountId')
WHERE organization_account_id IS NULL;
