ALTER TABLE husonym_api.instance DROP COLUMN IF EXISTS sending_since;
ALTER TABLE husonym_api.usage_reports DROP COLUMN IF EXISTS last_attempt_at;
ALTER TABLE husonym_api.usage_reports DROP COLUMN IF EXISTS attempts;
ALTER TABLE husonym_api.usage_reports DROP COLUMN IF EXISTS sent_at;
