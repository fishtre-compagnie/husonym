-- The display identity of a user, as the identity provider presents it.
--
-- These are the standard OIDC claims (name, email, email_verified, picture). They are
-- written at sign-in, from the token or from the provider's userinfo endpoint, so that
-- showing the members of an account no longer requires calling a provider's own
-- administration API -- which is not OIDC, exists only for Auth0 and Keycloak, and is a
-- single client for the whole deployment.
--
-- They are display values, never a decision: a user is identified by its provider_sub,
-- and email_verified is what an invite has to be judged on, never email alone.
ALTER TABLE husonym_api.user_identity_provider_associations
ADD COLUMN IF NOT EXISTS name varchar NULL,
ADD COLUMN IF NOT EXISTS email varchar NULL,
-- False, not null: a row written before this migration carries no proof, and the absence
-- of proof is not a verified address.
ADD COLUMN IF NOT EXISTS email_verified boolean NOT NULL DEFAULT false,
ADD COLUMN IF NOT EXISTS picture varchar NULL;

COMMENT ON COLUMN husonym_api.user_identity_provider_associations.email_verified
  IS 'Whether the provider asserts it verified this address. False means no proof, including for rows written before the column existed.';
