DROP TABLE IF EXISTS husonym_api.usage_reports;
DROP TABLE IF EXISTS husonym_api.user_activity;
DROP INDEX IF EXISTS husonym_api.run_usage_counted_at_idx;
ALTER TABLE husonym_api.run_usage DROP COLUMN IF EXISTS source_version_major;
ALTER TABLE husonym_api.run_usage DROP COLUMN IF EXISTS tables_uncounted;
ALTER TABLE husonym_api.run_usage DROP COLUMN IF EXISTS settled_at;
