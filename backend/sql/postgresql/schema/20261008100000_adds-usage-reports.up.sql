-- What the daily usage report needs beyond the counters of the first usage migration. Counts,
-- days and identifiers only: never a name, a query or a message a customer entered.

-- When a run was settled by the instance rather than ended by the worker: such a run counts for
-- the day it was settled.
ALTER TABLE husonym_api.run_usage
  ADD COLUMN IF NOT EXISTS settled_at timestamptz NULL;

-- How many tables of the run reported no row count.
ALTER TABLE husonym_api.run_usage
  ADD COLUMN IF NOT EXISTS tables_uncounted bigint NOT NULL DEFAULT 0;

-- The major version of the source engine, as the worker saw it. Null when it is not known.
ALTER TABLE husonym_api.run_usage
  ADD COLUMN IF NOT EXISTS source_version_major text NULL;

-- A run counts for the UTC day of this moment.
CREATE INDEX IF NOT EXISTS run_usage_counted_at_idx
  ON husonym_api.run_usage ((COALESCE(ended_at, settled_at)));

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
