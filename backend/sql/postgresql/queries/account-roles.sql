-- The role a member holds in an account is a row of husonym_api.casbin_rule: 'g', the member,
-- the role, the account. These two statements replace it, in one transaction.

-- Held until the transaction ends, so that two changes of the role of one member in one
-- account, wherever they are asked, are made one after the other: the second sees what the
-- first wrote.
-- name: LockAccountRole :exec
SELECT pg_advisory_xact_lock(
  hashtextextended(sqlc.arg('member')::text || ' ' || sqlc.arg('account')::text, 0)
);

-- Leaves the member that role in the account and no other. A row that already says so is kept
-- as it is.
-- name: ReplaceAccountRole :exec
WITH removed AS (
  DELETE FROM husonym_api.casbin_rule
  WHERE p_type = 'g'
    AND v0 = sqlc.arg('member')
    AND v2 = sqlc.arg('account')
    AND v1 <> sqlc.arg('role')
)
INSERT INTO husonym_api.casbin_rule (p_type, v0, v1, v2)
VALUES ('g', sqlc.arg('member'), sqlc.arg('role'), sqlc.arg('account'))
ON CONFLICT DO NOTHING;

-- Tells whether the member holds a role in the account, whichever.
-- name: HasAccountRole :one
SELECT EXISTS (
  SELECT 1 FROM husonym_api.casbin_rule
  WHERE p_type = 'g'
    AND v0 = sqlc.arg('member')::text
    AND v2 = sqlc.arg('account')::text
);

-- Gives the member that role in the account when the member holds none there, and changes
-- nothing otherwise. It says how many rows it wrote. After LockAccountRole, in a transaction
-- that reads what was committed before each of its statements, no role can be given to the
-- member between its look and its write.
-- name: AddAccountRoleIfNone :execrows
INSERT INTO husonym_api.casbin_rule (p_type, v0, v1, v2)
SELECT 'g', sqlc.arg('member')::text, sqlc.arg('role')::text, sqlc.arg('account')::text
WHERE NOT EXISTS (
  SELECT 1 FROM husonym_api.casbin_rule
  WHERE p_type = 'g'
    AND v0 = sqlc.arg('member')::text
    AND v2 = sqlc.arg('account')::text
)
ON CONFLICT DO NOTHING;
