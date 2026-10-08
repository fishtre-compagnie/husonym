-- name: InsertOperatorAction :exec
INSERT INTO controlplane.operator_actions (at, operator, action, customer_id, license_id, detail)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: ListOperatorActions :many
-- The newest lines first, with the name the customer bears today.
SELECT a.id, a.at, a.operator, a.action, a.customer_id, c.name AS customer_name, a.license_id, a.detail
FROM controlplane.operator_actions a
LEFT JOIN controlplane.customers c ON c.id = a.customer_id
ORDER BY a.at DESC, a.id DESC
LIMIT sqlc.arg(at_most);

-- name: GetLicenseIssuing :one
-- Who issued a license from the console, and when. No row for a license that came otherwise.
SELECT operator, at
FROM controlplane.operator_actions
WHERE license_id = $1 AND action IN ('license_issued', 'license_renewed')
ORDER BY id
LIMIT 1;

-- name: InsertCustomer :one
-- No row when a customer already has the external id.
INSERT INTO controlplane.customers (external_id, name, note, created_at, updated_at)
VALUES (sqlc.arg(external_id), sqlc.arg(name), sqlc.arg(note), sqlc.arg(now), sqlc.arg(now))
ON CONFLICT (external_id) DO NOTHING
RETURNING id;

-- name: GetCustomerNameAndNoteForUpdate :one
-- What an edit may change, read under the lock of the edit.
SELECT name, note
FROM controlplane.customers
WHERE id = $1
FOR UPDATE;

-- name: UpdateCustomer :exec
-- The external id is not among what changes: the keys issued carry it.
UPDATE controlplane.customers
SET name = sqlc.arg(name), note = sqlc.arg(note), updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id);

-- name: GetCustomerIDByExternalID :one
SELECT id
FROM controlplane.customers
WHERE external_id = $1;

-- name: GetLicenseCustomer :one
SELECT customer_id
FROM controlplane.licenses
WHERE id = $1;

-- name: GetLicenseKey :one
-- The encoded key of a license: read only to show it again to the operator, which is journaled.
SELECT encoded, customer_id
FROM controlplane.licenses
WHERE id = $1;
