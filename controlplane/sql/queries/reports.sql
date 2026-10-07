-- name: UpsertInstance :exec
-- An instance keeps the moment it was first seen. What tells its latest state is only replaced
-- by the report of a later day, so that a late report of an earlier day changes nothing. A
-- report without the diagnostics does not erase the kind of installation already known.
INSERT INTO controlplane.instances (
    id, first_seen_at, last_seen_at, last_report_day, last_license_id, husonym_version, install_kind
) VALUES (
    sqlc.arg(id), sqlc.arg(seen_at), sqlc.arg(seen_at), sqlc.arg(report_day), sqlc.arg(license_id),
    sqlc.arg(husonym_version), sqlc.narg(install_kind)
)
ON CONFLICT (id) DO UPDATE SET
    last_seen_at = EXCLUDED.last_seen_at,
    last_report_day = EXCLUDED.last_report_day,
    last_license_id = EXCLUDED.last_license_id,
    husonym_version = EXCLUDED.husonym_version,
    install_kind = COALESCE(EXCLUDED.install_kind, controlplane.instances.install_kind)
WHERE EXCLUDED.last_report_day > controlplane.instances.last_report_day;

-- name: InsertUsageReport :execrows
-- The first report received for an instance and a day is the one that stays.
INSERT INTO controlplane.usage_reports (instance_id, day, license_id, document, seal, received_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (instance_id, day) DO NOTHING;

-- name: CountUsageReportConflict :execrows
-- Counts a report that differs from the one already stored for its instance and day. No row is
-- touched when the document is the same: that is a repeat.
UPDATE controlplane.usage_reports
SET conflicts = conflicts + 1, last_conflict_at = sqlc.arg(at)
WHERE instance_id = sqlc.arg(instance_id) AND day = sqlc.arg(day) AND document <> sqlc.arg(document);

-- name: CountSealRejection :exec
INSERT INTO controlplane.seal_rejections (license_id, day, count, last_at)
VALUES ($1, $2, 1, $3)
ON CONFLICT (license_id, day) DO UPDATE SET
    count = controlplane.seal_rejections.count + 1,
    last_at = EXCLUDED.last_at;

-- name: PendingReportExists :one
SELECT EXISTS (
    SELECT 1 FROM controlplane.pending_reports
    WHERE key_fingerprint = $1 AND instance_id = $2 AND day = $3
);

-- name: CountPendingReports :one
SELECT
    count(*) FILTER (WHERE key_fingerprint = $1) AS for_fingerprint,
    count(*) AS total
FROM controlplane.pending_reports;

-- name: InsertPendingReport :exec
INSERT INTO controlplane.pending_reports (key_fingerprint, instance_id, day, document, seal, received_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (key_fingerprint, instance_id, day) DO NOTHING;

-- name: ListPendingReports :many
SELECT key_fingerprint, instance_id, day, document, seal, received_at
FROM controlplane.pending_reports
WHERE key_fingerprint = $1
ORDER BY day, instance_id;

-- name: DeletePendingReport :execrows
DELETE FROM controlplane.pending_reports
WHERE key_fingerprint = $1 AND instance_id = $2 AND day = $3;

-- name: ListPendingFingerprintsNowKnown :many
SELECT DISTINCT p.key_fingerprint
FROM controlplane.pending_reports p
JOIN controlplane.licenses l ON l.key_fingerprint = p.key_fingerprint
ORDER BY p.key_fingerprint;

-- name: PurgePendingReports :execrows
DELETE FROM controlplane.pending_reports
WHERE received_at < $1;
