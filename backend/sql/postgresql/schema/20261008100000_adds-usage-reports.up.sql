-- What the daily usage report needs beyond the counters of the first usage migration. Counts,
-- days and identifiers only: never a name, a query or a message a customer entered.

-- When the API recorded the end of the run, whichever way it learned of it. A run counts for
-- the UTC day of this moment; a run still running has none.
ALTER TABLE husonym_api.run_usage
  ADD COLUMN IF NOT EXISTS recorded_at timestamptz NULL;

-- The runs that ended before this migration: their end is the closest thing known.
UPDATE husonym_api.run_usage
SET recorded_at = COALESCE(ended_at, CURRENT_TIMESTAMP)
WHERE status <> 'running' AND recorded_at IS NULL;

-- How many tables of the run reported no row count.
ALTER TABLE husonym_api.run_usage
  ADD COLUMN IF NOT EXISTS tables_uncounted bigint NOT NULL DEFAULT 0;

-- The major version of the source engine, as the worker saw it. Null when it is not known.
ALTER TABLE husonym_api.run_usage
  ADD COLUMN IF NOT EXISTS source_version_major text NULL;

-- The runs of a day are looked up by the moment their end was recorded.
CREATE INDEX IF NOT EXISTS run_usage_recorded_at_idx
  ON husonym_api.run_usage (recorded_at);

-- The last day (UTC) each user was seen. There is no foreign key to users: the count of active
-- users must not depend on the row of a user that is gone.
CREATE TABLE IF NOT EXISTS husonym_api.user_activity (
  user_id uuid PRIMARY KEY,
  last_seen_on date NOT NULL
);

COMMENT ON TABLE husonym_api.user_activity
  IS 'Stores the last day each user was seen, to count the users active over a period';

-- The report of each day, kept as it was prepared.
CREATE TABLE IF NOT EXISTS husonym_api.usage_reports (
  day date PRIMARY KEY,
  -- The exact JSON, byte for byte.
  document text NOT NULL,
  seal text NOT NULL,
  key_fingerprint text NOT NULL,
  prepared_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP
);

COMMENT ON TABLE husonym_api.usage_reports
  IS 'Stores the usage report of each day, as prepared';
