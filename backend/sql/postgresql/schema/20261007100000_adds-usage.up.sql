-- Local usage counters. They hold counts and identifiers only: never the name of a table, a
-- schema, a column or a job, and never the text of an error.

-- The identity of the instance: one row, made here, that no one writes again.
CREATE TABLE IF NOT EXISTS husonym_api.instance (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,

  -- Holds the table to one row: a second insert collides with the first.
  singleton boolean NOT NULL DEFAULT true UNIQUE,
  CONSTRAINT instance_singleton CHECK (singleton)
);

INSERT INTO husonym_api.instance DEFAULT VALUES ON CONFLICT DO NOTHING;

COMMENT ON TABLE husonym_api.instance
  IS 'Stores the identity of the instance: a single row';

-- One row per run. There is no foreign key to the jobs or the accounts: the usage of a run
-- outlives the job that made it.
CREATE TABLE IF NOT EXISTS husonym_api.run_usage (
  -- The id of the run in the orchestrator.
  run_id text PRIMARY KEY,
  account_id uuid NOT NULL,
  job_id uuid NOT NULL,

  job_kind text NOT NULL,
  CONSTRAINT run_usage_job_kind_known
    CHECK (job_kind IN ('sync', 'generate', 'ai_generate', 'pii_detect')),

  -- 'running' until the run ends or is settled.
  status text NOT NULL,
  CONSTRAINT run_usage_status_known
    CHECK (status IN ('running', 'completed', 'failed', 'canceled', 'terminated', 'timed_out')),

  started_at timestamptz NOT NULL,
  -- Null while the run is open, and for a run settled without a known end.
  ended_at timestamptz NULL,

  rows_read bigint NOT NULL DEFAULT 0,
  rows_discarded bigint NOT NULL DEFAULT 0,
  retries bigint NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS run_usage_account_id_ended_at_idx
  ON husonym_api.run_usage (account_id, ended_at);
CREATE INDEX IF NOT EXISTS run_usage_status_started_at_idx
  ON husonym_api.run_usage (status, started_at);

COMMENT ON TABLE husonym_api.run_usage
  IS 'Stores the usage of each run: counts and times, nothing a customer entered';

-- How many times a day (UTC) a gate of the license refused an account.
CREATE TABLE IF NOT EXISTS husonym_api.gate_refusals_daily (
  day date NOT NULL,
  account_id uuid NOT NULL,
  gate text NOT NULL,
  count bigint NOT NULL,

  PRIMARY KEY (day, account_id, gate)
);

COMMENT ON TABLE husonym_api.gate_refusals_daily
  IS 'Stores the daily count of license refusals per account and gate';
