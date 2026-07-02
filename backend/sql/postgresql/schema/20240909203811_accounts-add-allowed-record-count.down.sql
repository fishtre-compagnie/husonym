ALTER TABLE husonym_api.accounts
DROP CONSTRAINT no_negative_max_allowed_records;

ALTER TABLE husonym_api.accounts
DROP COLUMN max_allowed_records;
