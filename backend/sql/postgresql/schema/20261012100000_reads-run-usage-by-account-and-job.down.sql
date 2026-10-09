CREATE INDEX IF NOT EXISTS run_usage_account_id_ended_at_idx
  ON husonym_api.run_usage (account_id, ended_at);

DROP INDEX IF EXISTS husonym_api.run_usage_job_id_recorded_at_idx;
DROP INDEX IF EXISTS husonym_api.run_usage_account_id_recorded_at_idx;
