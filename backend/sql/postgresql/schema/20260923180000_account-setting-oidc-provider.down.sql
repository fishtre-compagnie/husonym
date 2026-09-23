-- An account that declared a provider cannot keep the row once the column stops
-- recognizing the variant: setting_type would be NULL and the CHECK would refuse it.
DELETE FROM husonym_api.account_settings
WHERE config->'oidcProvider' IS NOT NULL;

ALTER TABLE husonym_api.account_settings
DROP COLUMN IF EXISTS setting_type;

ALTER TABLE husonym_api.account_settings
ADD COLUMN setting_type text GENERATED ALWAYS AS (
  CASE
    WHEN config->'anonymizationConsistency' IS NOT NULL THEN 'anonymization_consistency'
    ELSE NULL
  END
) STORED;

ALTER TABLE husonym_api.account_settings
ADD CONSTRAINT account_settings_type_not_null CHECK (setting_type IS NOT NULL);

ALTER TABLE husonym_api.account_settings
ADD CONSTRAINT account_settings_one_per_type UNIQUE (account_id, setting_type);
