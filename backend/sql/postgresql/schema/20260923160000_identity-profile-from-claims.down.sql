ALTER TABLE husonym_api.user_identity_provider_associations
DROP COLUMN IF EXISTS name,
DROP COLUMN IF EXISTS email,
DROP COLUMN IF EXISTS email_verified,
DROP COLUMN IF EXISTS picture;
