-- The organization of the instance: the one account the people who sign in work in. Null until
-- one is retained. The account it names cannot be deleted while it is retained.
ALTER TABLE husonym_api.instance
  ADD COLUMN IF NOT EXISTS organization_account_id uuid NULL
  REFERENCES husonym_api.accounts(id);
