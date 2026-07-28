ALTER TABLE husonym_api.jobs
ADD COLUMN IF NOT EXISTS virtual_foreign_keys jsonb NOT NULL DEFAULT '[]'::jsonb;
