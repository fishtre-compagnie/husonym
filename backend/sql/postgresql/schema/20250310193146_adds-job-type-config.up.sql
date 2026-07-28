ALTER TABLE husonym_api.jobs
ADD COLUMN jobtype_config JSONB NOT NULL DEFAULT '{}'::JSONB;
