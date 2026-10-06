-- The license keys belong to the instance: there is no account here.

-- Held until the transaction ends, so that two keys given at the same moment, wherever they
-- are asked, are looked at one after the other: the second sees what the first wrote.
-- name: LockLicenseKeys :exec
SELECT pg_advisory_xact_lock(hashtextextended('license_keys', 0));

-- The key in force: the latest one issued, the latest one stored when two were issued at the
-- same instant. No row means the instance has no key.
-- name: GetCurrentLicenseKey :one
SELECT *
FROM husonym_api.license_keys
ORDER BY issued_at DESC, created_at DESC
LIMIT 1;

-- name: InsertLicenseKey :one
INSERT INTO husonym_api.license_keys (
  key, license_id, issued_at, origin, created_by_user_id
) VALUES (
  $1, $2, $3, $4, $5
)
RETURNING *;
