-- What kept a run from completing, and the step it was at. Both columns hold a member of a
-- closed list, never the message of an error nor anything a customer entered. They are null
-- for a run that completed or still runs, and both set for every other run.
ALTER TABLE husonym_api.run_usage
  ADD COLUMN IF NOT EXISTS error_category text NULL;
ALTER TABLE husonym_api.run_usage
  ADD COLUMN IF NOT EXISTS error_step text NULL;

-- The runs that did not complete before this migration: nothing was told of their error, so
-- their status alone gives the category, and the step is not known.
UPDATE husonym_api.run_usage
SET error_category = CASE status
    WHEN 'canceled' THEN 'canceled'
    WHEN 'timed_out' THEN 'timeout'
    ELSE 'other'
  END,
  error_step = 'other'
WHERE status IN ('failed', 'canceled', 'terminated', 'timed_out') AND error_category IS NULL;

ALTER TABLE husonym_api.run_usage
  DROP CONSTRAINT IF EXISTS run_usage_error_category_known;
ALTER TABLE husonym_api.run_usage
  ADD CONSTRAINT run_usage_error_category_known
    CHECK (error_category IN (
      'connection_refused', 'authentication_refused', 'timeout', 'constraint_violated',
      'insufficient_privileges', 'object_missing', 'type_mismatch', 'resources_exhausted',
      'canceled', 'license', 'other'
    ));

ALTER TABLE husonym_api.run_usage
  DROP CONSTRAINT IF EXISTS run_usage_error_step_known;
ALTER TABLE husonym_api.run_usage
  ADD CONSTRAINT run_usage_error_step_known
    CHECK (error_step IN (
      'preflight', 'schema_init', 'table_sync', 'hooks', 'integrity_check', 'other'
    ));

-- A category never goes without its step, nor a step without its category.
ALTER TABLE husonym_api.run_usage
  DROP CONSTRAINT IF EXISTS run_usage_error_whole;
ALTER TABLE husonym_api.run_usage
  ADD CONSTRAINT run_usage_error_whole
    CHECK ((error_category IS NULL) = (error_step IS NULL));
