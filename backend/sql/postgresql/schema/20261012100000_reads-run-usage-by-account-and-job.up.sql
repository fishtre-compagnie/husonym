-- The usage pages read the runs of one account, or of one of its jobs, whose end was recorded in
-- a period. Indexes only: no row and no column changes.

-- The runs of an account in a period (its totals, its days, its jobs and its errors) get no
-- index of their own: they are read by the moment they were recorded, through
-- run_usage_recorded_at_idx, as the runs of a day are. An instance holds one organization, whose
-- account holds nearly every run, and an index by account would not be chosen for it.

-- The runs of a job in a period: its totals, its days and its latest runs, the most recently
-- recorded first.
CREATE INDEX IF NOT EXISTS run_usage_job_id_recorded_at_idx
  ON husonym_api.run_usage (job_id, recorded_at);

-- Made for these pages before the day of a run became the one its end was recorded on: no query
-- reads the runs of an account by their end.
DROP INDEX IF EXISTS husonym_api.run_usage_account_id_ended_at_idx;
