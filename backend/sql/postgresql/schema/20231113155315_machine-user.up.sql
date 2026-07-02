ALTER TABLE husonym_api.users
ADD COLUMN IF NOT EXISTS user_type smallint not null default 0;

ALTER TABLE husonym_api.account_api_keys
ADD COLUMN IF NOT EXISTS user_id uuid null;

ALTER TABLE husonym_api.account_api_keys
ADD CONSTRAINT fk_account_api_keys_user_id_users_id FOREIGN KEY (user_id) REFERENCES husonym_api.users(id);
