-- The language a user chose for the interface. Null means the user follows the default of
-- the instance, which is English. The column holds a member of a closed list: adding a
-- language to the interface adds it to this list too, so the database never keeps a value
-- the interface cannot render.
ALTER TABLE husonym_api.users
  ADD COLUMN IF NOT EXISTS locale text NULL;

ALTER TABLE husonym_api.users
  DROP CONSTRAINT IF EXISTS users_locale_known;
ALTER TABLE husonym_api.users
  ADD CONSTRAINT users_locale_known
    CHECK (locale IN ('en', 'fr'));
