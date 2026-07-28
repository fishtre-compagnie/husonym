ALTER TABLE husonym_api.account_invites DROP CONSTRAINT fk_invites_user_id;

ALTER TABLE husonym_api.account_invites
ADD CONSTRAINT fk_invites_user_id FOREIGN KEY (sender_user_id)
REFERENCES husonym_api.users(id) ON DELETE RESTRICT;

ALTER TABLE husonym_api.account_invites ALTER COLUMN sender_user_id SET NOT NULL;
