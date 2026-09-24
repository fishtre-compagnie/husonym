-- Rolling back drops every key's permissions, and the code before them lets every key do
-- everything. Migrating up again gives every key every permission, as the column's default:
-- keys that had been narrowed come back whole, and must be narrowed again — recreated with only
-- what they need.
ALTER TABLE husonym_api.account_api_keys DROP COLUMN IF EXISTS permissions;
