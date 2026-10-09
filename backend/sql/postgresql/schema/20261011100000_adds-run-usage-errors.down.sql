ALTER TABLE husonym_api.run_usage DROP CONSTRAINT IF EXISTS run_usage_error_whole;
ALTER TABLE husonym_api.run_usage DROP CONSTRAINT IF EXISTS run_usage_error_step_known;
ALTER TABLE husonym_api.run_usage DROP CONSTRAINT IF EXISTS run_usage_error_category_known;
ALTER TABLE husonym_api.run_usage DROP COLUMN IF EXISTS error_step;
ALTER TABLE husonym_api.run_usage DROP COLUMN IF EXISTS error_category;
