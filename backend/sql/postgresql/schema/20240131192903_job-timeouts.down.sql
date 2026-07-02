ALTER TABLE husonym_api.jobs
DROP COLUMN IF EXISTS run_timeout,
DROP COLUMN IF EXISTS sync_options;
