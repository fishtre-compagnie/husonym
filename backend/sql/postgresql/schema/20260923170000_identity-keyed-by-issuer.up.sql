-- An identity is (issuer, subject), not a subject alone.
--
-- Whoever declares an identity provider controls the subjects it issues. As long as a
-- deployment has exactly one provider that is nobody's problem; the moment an account can
-- declare its own, a subject alone lets its administrator mint the subject of an existing
-- user and become them. The key has to carry the issuer before that becomes possible, not
-- after.
--
-- The empty string means "recorded before issuers were". It is not a backfill: a SQL
-- migration cannot read AUTH_EXPECTED_ISS, and guessing an issuer is exactly the mistake
-- this column exists to prevent. Such a row is adopted at its owner's next sign-in, and
-- only by the deployment's own issuer -- see SetUserByAuthSub.
ALTER TABLE husonym_api.user_identity_provider_associations
ADD COLUMN IF NOT EXISTS provider_iss varchar NOT NULL DEFAULT '';

-- The old constraint still carries the name it was created with, before the column was
-- renamed from auth0_provider_id to provider_sub (migration 20231226192859).
ALTER TABLE husonym_api.user_identity_provider_associations
DROP CONSTRAINT IF EXISTS user_identity_provider_associations_auth0_provider_id_key;

ALTER TABLE husonym_api.user_identity_provider_associations
ADD CONSTRAINT user_identity_provider_associations_iss_sub_key
  UNIQUE (provider_iss, provider_sub);

COMMENT ON COLUMN husonym_api.user_identity_provider_associations.provider_iss
  IS 'The iss claim of the token this identity was seen in. Empty for a row recorded before issuers were, adopted at the next sign-in by the deployment issuer only.';

-- An invitation names the issuer it may be accepted from.
--
-- Matching an invitation on the email claim alone is what lets an account that declares
-- its own provider mint a token for someone else's address and walk into an account it
-- was never invited to. Recording the issuer at creation costs nothing today, where there
-- is one, and is the check that closes that door when there are several.
ALTER TABLE husonym_api.account_invites
ADD COLUMN IF NOT EXISTS provider_iss varchar NOT NULL DEFAULT '';

COMMENT ON COLUMN husonym_api.account_invites.provider_iss
  IS 'The issuer a token must carry to accept this invitation. Empty for an invitation created before issuers were recorded, which any issuer of the deployment may still accept.';
