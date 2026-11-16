ALTER TABLE husonym_api.users DROP CONSTRAINT IF EXISTS users_locale_known;
ALTER TABLE husonym_api.users DROP COLUMN IF EXISTS locale;
