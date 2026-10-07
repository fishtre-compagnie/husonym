-- What became of each usage report: when it was sent, and how many times, and when last, it was
-- tried. Counts and moments only.
ALTER TABLE husonym_api.usage_reports
  ADD COLUMN IF NOT EXISTS sent_at timestamptz NULL;

ALTER TABLE husonym_api.usage_reports
  ADD COLUMN IF NOT EXISTS attempts integer NOT NULL DEFAULT 0;

ALTER TABLE husonym_api.usage_reports
  ADD COLUMN IF NOT EXISTS last_attempt_at timestamptz NULL;

-- Since when the instance sends its usage report. Null when it does not send.
ALTER TABLE husonym_api.instance
  ADD COLUMN IF NOT EXISTS sending_since timestamptz NULL;
