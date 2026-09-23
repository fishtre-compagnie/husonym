ALTER TABLE husonym_api.account_invites
DROP COLUMN IF EXISTS provider_iss;

ALTER TABLE husonym_api.user_identity_provider_associations
DROP CONSTRAINT IF EXISTS user_identity_provider_associations_iss_sub_key;

-- Two identities that differ only by their issuer cannot both survive the return to a
-- subject-only key. Keeping the oldest of each is the only choice that does not pick a
-- winner on something other than seniority.
DELETE FROM husonym_api.user_identity_provider_associations a
USING husonym_api.user_identity_provider_associations b
WHERE a.provider_sub = b.provider_sub
  AND (a.created_at, a.id) > (b.created_at, b.id);

ALTER TABLE husonym_api.user_identity_provider_associations
ADD CONSTRAINT user_identity_provider_associations_auth0_provider_id_key
  UNIQUE (provider_sub);

ALTER TABLE husonym_api.user_identity_provider_associations
DROP COLUMN IF EXISTS provider_iss;
