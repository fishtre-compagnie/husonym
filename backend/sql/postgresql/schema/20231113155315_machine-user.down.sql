ALTER TABLE husonym_api.users
DROP COLUMN IF EXISTS user_type;

ALTER TABLE husonym_api.account_api_keys
DROP COLUMN IF EXISTS user_id;
