-- name: GetLatestSuccessor :one
-- The last license of the chain of successors of a license, no further than max_depth successors
-- away. The index that gives a license one successor at most makes the chain a line; the depth
-- ends the walk of one that would loop. cut_short tells the license returned has a successor of
-- its own, which only happens at the bound.
WITH RECURSIVE chain AS (
    SELECT s.id, 1 AS depth
    FROM controlplane.licenses s
    WHERE s.succeeds_license_id = sqlc.arg(license_id)::text
    UNION ALL
    SELECT s.id, c.depth + 1
    FROM controlplane.licenses s
    JOIN chain c ON s.succeeds_license_id = c.id
    WHERE c.depth < sqlc.arg(max_depth)::int
)
SELECT
    l.id, l.encoded, l.telemetry,
    EXISTS (SELECT 1 FROM controlplane.licenses s WHERE s.succeeds_license_id = l.id) AS cut_short
FROM chain c
JOIN controlplane.licenses l ON l.id = c.id
ORDER BY c.depth DESC
LIMIT 1;

-- name: UpsertRenewalAsk :exec
-- The last ask of an instance for the renewal of a license, and what was served to it: an ask
-- that is served nothing leaves what was served before. A license keeps max_instances rows at
-- most, those of the instances that asked last: an instance that is not yet there takes the place
-- of the one that asked the longest ago, and of any row left over the cap. Nothing is locked: asks
-- made at once may leave a few rows over the cap, which the next new instance removes.
WITH replaced AS (
    DELETE FROM controlplane.renewal_asks d
    WHERE d.license_id = sqlc.arg(license_id)::text
        AND NOT EXISTS (
            SELECT 1 FROM controlplane.renewal_asks
            WHERE license_id = sqlc.arg(license_id)::text AND instance_id = sqlc.arg(instance_id)::text
        )
        AND d.instance_id IN (
            SELECT k.instance_id FROM controlplane.renewal_asks k
            WHERE k.license_id = sqlc.arg(license_id)::text
            ORDER BY k.last_asked_at DESC, k.instance_id
            OFFSET GREATEST(sqlc.arg(max_instances)::int - 1, 0)
        )
)
INSERT INTO controlplane.renewal_asks AS a (
    license_id, instance_id, last_asked_at, last_served_license_id, last_served_at
)
VALUES (
    sqlc.arg(license_id)::text, sqlc.arg(instance_id)::text, sqlc.arg(at)::timestamptz,
    sqlc.narg(served_license_id)::text,
    CASE WHEN sqlc.narg(served_license_id)::text IS NULL THEN NULL ELSE sqlc.arg(at)::timestamptz END
)
ON CONFLICT (license_id, instance_id) DO UPDATE SET
    last_asked_at = EXCLUDED.last_asked_at,
    last_served_license_id = COALESCE(EXCLUDED.last_served_license_id, a.last_served_license_id),
    last_served_at = COALESCE(EXCLUDED.last_served_at, a.last_served_at);

-- name: ListRenewalAsksOfLicense :many
-- The instances that asked for the renewal of a license, the one that asked last first.
SELECT instance_id, last_asked_at, last_served_license_id, last_served_at
FROM controlplane.renewal_asks
WHERE license_id = $1
ORDER BY last_asked_at DESC, instance_id;
