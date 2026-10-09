-- name: UpsertCustomer :one
-- An existing customer is left as it is: a name is never overwritten.
INSERT INTO controlplane.customers (external_id, name)
VALUES ($1, $2)
ON CONFLICT (external_id) DO UPDATE SET external_id = controlplane.customers.external_id
RETURNING id;

-- name: LockLicenseID :exec
-- Held until the transaction ends: two writers of one license id go one after the other. Without
-- it, two inserts of the same license at once may meet on the unique constraint of the encoded key
-- or of its fingerprint before they meet on the id, and that one is an error, not a conflict left
-- alone.
SELECT pg_advisory_xact_lock(hashtextextended('controlplane.licenses:' || sqlc.arg(id)::text, 0));

-- name: InsertLicense :execrows
INSERT INTO controlplane.licenses (
    id, customer_id, encoded, key_fingerprint, kid, plan, features, limits, telemetry,
    issued_at, expires_at, grace_days, signing_key_fingerprint, note, origin, succeeds_license_id
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9,
    $10, $11, $12, $13, $14, $15, $16
)
ON CONFLICT (id) DO NOTHING;

-- name: GetLicenseByFingerprint :one
SELECT id, encoded, telemetry
FROM controlplane.licenses
WHERE key_fingerprint = $1;
