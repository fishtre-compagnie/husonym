-- The generated column has to learn the new variant, or the first write of an OIDC
-- setting lands a row whose setting_type is NULL and the CHECK refuses it -- in
-- production, at the first use.
--
-- A generated column cannot be altered in place, so it is dropped and recreated. The
-- column is derived, never written: no data is lost, and every existing row gets the same
-- value back.
ALTER TABLE husonym_api.account_settings
DROP COLUMN IF EXISTS setting_type;

ALTER TABLE husonym_api.account_settings
ADD COLUMN setting_type text GENERATED ALWAYS AS (
  CASE
    WHEN config->'anonymizationConsistency' IS NOT NULL THEN 'anonymization_consistency'
    WHEN config->'oidcProvider' IS NOT NULL THEN 'oidc_provider'
    ELSE NULL
  END
) STORED;

ALTER TABLE husonym_api.account_settings
ADD CONSTRAINT account_settings_type_not_null CHECK (setting_type IS NOT NULL);

ALTER TABLE husonym_api.account_settings
ADD CONSTRAINT account_settings_one_per_type UNIQUE (account_id, setting_type);
