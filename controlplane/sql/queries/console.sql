-- name: ListCustomerSummaries :many
-- The nearest expiry is the next one to come; when every license has expired, the last one.
SELECT
    c.id,
    c.external_id,
    c.name,
    (SELECT count(*) FROM controlplane.licenses l WHERE l.customer_id = c.id) AS licenses,
    (
        SELECT count(*)
        FROM controlplane.instances i
        JOIN controlplane.licenses l ON l.id = i.license_id
        WHERE l.customer_id = c.id AND i.last_report_day >= sqlc.arg(seen_since)
    ) AS recent_instances,
    (
        SELECT COALESCE(min(l.expires_at) FILTER (WHERE l.expires_at >= sqlc.arg(now)), max(l.expires_at))
        FROM controlplane.licenses l
        WHERE l.customer_id = c.id
    )::timestamptz AS nearest_expiry
FROM controlplane.customers c
ORDER BY c.name, c.external_id;

-- name: GetCustomer :one
SELECT id, external_id, name, note, created_at, updated_at
FROM controlplane.customers
WHERE id = $1;

-- name: ListLicensesOfCustomer :many
SELECT
    l.id, l.customer_id, c.name AS customer_name, l.plan, l.telemetry, l.expires_at, l.grace_days,
    EXISTS (SELECT 1 FROM controlplane.licenses s WHERE s.succeeds_license_id = l.id) AS has_successor
FROM controlplane.licenses l
JOIN controlplane.customers c ON c.id = l.customer_id
WHERE l.customer_id = $1
ORDER BY l.expires_at DESC, l.id;

-- name: GetLicenseDetail :one
-- Every column of a license but the encoded key.
SELECT
    l.id, l.customer_id, c.name AS customer_name, l.key_fingerprint, l.kid, l.plan, l.features, l.limits,
    l.telemetry, l.issued_at, l.expires_at, l.grace_days, l.signing_key_fingerprint, l.note,
    l.succeeds_license_id, l.origin, l.created_at
FROM controlplane.licenses l
JOIN controlplane.customers c ON c.id = l.customer_id
WHERE l.id = $1;

-- name: ListSuccessorsOfLicense :many
SELECT id
FROM controlplane.licenses
WHERE succeeds_license_id = sqlc.arg(license_id)::text
ORDER BY id;

-- name: ListInstancesOfCustomer :many
SELECT sqlc.embed(i)
FROM controlplane.instances i
JOIN controlplane.licenses l ON l.id = i.license_id
WHERE l.customer_id = $1
ORDER BY i.last_report_day DESC, i.license_id, i.instance_id;

-- name: ListInstancesOfLicense :many
SELECT sqlc.embed(i)
FROM controlplane.instances i
WHERE i.license_id = $1
ORDER BY i.last_report_day DESC, i.instance_id;

-- name: GetInstance :one
SELECT sqlc.embed(i), l.customer_id, c.name AS customer_name, l.limits
FROM controlplane.instances i
JOIN controlplane.licenses l ON l.id = i.license_id
JOIN controlplane.customers c ON c.id = l.customer_id
WHERE i.license_id = $1 AND i.instance_id = $2;

-- name: ListReportsOfInstance :many
-- The newest days first, no further than the cap of the list.
SELECT day, received_at, conflicts, document
FROM controlplane.usage_reports
WHERE license_id = sqlc.arg(license_id) AND instance_id = sqlc.arg(instance_id)
ORDER BY day DESC
LIMIT sqlc.arg(at_most);

-- name: GetUsageReport :one
-- A report, with the customer of its license.
SELECT
    r.license_id, r.instance_id, r.day, r.document, r.seal, r.received_at, r.conflicts, r.last_conflict_at,
    l.customer_id, c.name AS customer_name
FROM controlplane.usage_reports r
JOIN controlplane.licenses l ON l.id = r.license_id
JOIN controlplane.customers c ON c.id = l.customer_id
WHERE r.license_id = $1 AND r.instance_id = $2 AND r.day = $3;

-- name: ListPendingGroups :many
-- The pending reports of each fingerprint, and how many of them were received before old_before.
SELECT
    key_fingerprint,
    count(*) AS reports,
    count(*) FILTER (WHERE received_at < sqlc.arg(old_before)) AS old_reports,
    min(received_at)::timestamptz AS oldest,
    max(received_at)::timestamptz AS newest,
    array_agg(DISTINCT instance_id ORDER BY instance_id)::text[] AS instance_ids
FROM controlplane.pending_reports
GROUP BY key_fingerprint
ORDER BY min(received_at), key_fingerprint;

-- name: ListSilentCandidates :many
-- The instances whose last report is of a day in [from_day, before_day), with what tells whether
-- their license is in force and asks for reports: that is judged by the caller.
SELECT sqlc.embed(i), l.customer_id, c.name AS customer_name, l.telemetry, l.expires_at, l.grace_days
FROM controlplane.instances i
JOIN controlplane.licenses l ON l.id = i.license_id
JOIN controlplane.customers c ON c.id = l.customer_id
WHERE i.last_report_day >= sqlc.arg(from_day) AND i.last_report_day < sqlc.arg(before_day)
ORDER BY i.last_report_day, i.license_id, i.instance_id;

-- name: ListExpiringCandidates :many
-- The licenses no other one succeeds that expire before expires_before and whose grace period has
-- not run out at now. The caller stays the judge of the state of each: this only leaves out the
-- ones it would drop. The end of the grace period is counted as internal/license counts it
-- (Key.GraceEndsAt): the days of the key, default_grace_days when it does not say, none when they
-- are negative, of 24 hours each. Hours, not days: a day of an interval is as long as the day of
-- the session's time zone, which is 23 hours once a year.
SELECT l.id, l.customer_id, c.name AS customer_name, l.plan, l.telemetry, l.expires_at, l.grace_days
FROM controlplane.licenses l
JOIN controlplane.customers c ON c.id = l.customer_id
WHERE l.expires_at < sqlc.arg(expires_before)
    AND l.expires_at + GREATEST(COALESCE(l.grace_days, sqlc.arg(default_grace_days)::int), 0) * interval '24 hours'
        > sqlc.arg(now)::timestamptz
    AND NOT EXISTS (SELECT 1 FROM controlplane.licenses s WHERE s.succeeds_license_id = l.id)
ORDER BY l.expires_at, l.id;

-- name: ListSealRejections :many
-- The refused seals counted since a day, of one license or of all of them.
SELECT r.license_id, l.customer_id, c.name AS customer_name, r.day, r.count, r.last_at
FROM controlplane.seal_rejections r
JOIN controlplane.licenses l ON l.id = r.license_id
JOIN controlplane.customers c ON c.id = l.customer_id
WHERE r.day >= sqlc.arg(since_day)
    AND (sqlc.narg(license_id)::text IS NULL OR r.license_id = sqlc.narg(license_id))
ORDER BY r.day DESC, r.last_at DESC, r.license_id;

-- name: SumSealRejectionsOfDay :one
SELECT COALESCE(sum(count), 0)::bigint
FROM controlplane.seal_rejections
WHERE day = $1;

-- name: ListSharedLicenses :many
-- The licenses seen on more than one instance since a day.
SELECT
    l.id, l.customer_id, c.name AS customer_name, l.plan, l.telemetry, l.expires_at, l.grace_days,
    EXISTS (SELECT 1 FROM controlplane.licenses s WHERE s.succeeds_license_id = l.id) AS has_successor,
    seen.recent_instances
FROM controlplane.licenses l
JOIN controlplane.customers c ON c.id = l.customer_id
JOIN (
    SELECT license_id, count(*) AS recent_instances
    FROM controlplane.instances
    WHERE last_report_day >= sqlc.arg(seen_since)
    GROUP BY license_id
    HAVING count(*) > 1
) AS seen ON seen.license_id = l.id
ORDER BY c.name, l.id;
