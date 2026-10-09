-- The usage pages read the runs of one account, or of one of its jobs, whose end was recorded in
-- a period. Indexes only: no row and no column changes.

-- The runs of an account in a period: its totals, its days, its jobs and its errors. It serves
-- an account that holds a small share of the runs of the instance, whose page then costs what
-- the account ran and not what the instance ran. An account that holds most of them is read by
-- the moment its runs were recorded, as the runs of a day are.
CREATE INDEX IF NOT EXISTS run_usage_account_id_recorded_at_idx
  ON husonym_api.run_usage (account_id, recorded_at);

-- The runs of a job in a period: its totals, its days and its latest runs, the most recently
-- recorded first.
CREATE INDEX IF NOT EXISTS run_usage_job_id_recorded_at_idx
  ON husonym_api.run_usage (job_id, recorded_at);

-- Made for these pages before the day of a run became the one its end was recorded on: no query
-- reads the runs of an account by their end.
DROP INDEX IF EXISTS husonym_api.run_usage_account_id_ended_at_idx;
