ALTER TABLE
  husonym_api.accounts
ADD COLUMN IF NOT EXISTS temporal_config jsonb NOT NULL DEFAULT '{}'::jsonb;
