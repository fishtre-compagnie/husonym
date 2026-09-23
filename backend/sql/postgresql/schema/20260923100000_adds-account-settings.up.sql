CREATE TABLE IF NOT EXISTS husonym_api.account_settings (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

  account_id uuid NOT NULL,

  -- The serialized oneof. Its secret fields are already encrypted when they get here:
  -- field by field, so that what is not a secret stays readable for diagnosis.
  config jsonb NOT NULL,

  -- Derived from the oneof, as account_hooks.hook_type is: it is what makes the table
  -- readable and what carries the uniqueness.
  --
  -- The keys are the ones protojson writes, in lowerCamelCase — the same marshaling that
  -- put the config here. account_settings_type_matches_proto_test.go holds the migration
  -- and the proto to that agreement.
  setting_type text GENERATED ALWAYS AS (
    CASE
      WHEN config->'anonymizationConsistency' IS NOT NULL THEN 'anonymization_consistency'
      ELSE NULL
    END
  ) STORED,
  CONSTRAINT account_settings_type_not_null CHECK (setting_type IS NOT NULL),

  created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,

  created_by_user_id uuid NULL,
  updated_by_user_id uuid NULL,

  CONSTRAINT fk_account_settings_account
    FOREIGN KEY (account_id)
    REFERENCES husonym_api.accounts(id)
    ON DELETE CASCADE,

  CONSTRAINT fk_account_settings_created_by_user
    FOREIGN KEY (created_by_user_id)
    REFERENCES husonym_api.users(id)
    ON DELETE SET NULL,

  CONSTRAINT fk_account_settings_updated_by_user
    FOREIGN KEY (updated_by_user_id)
    REFERENCES husonym_api.users(id)
    ON DELETE SET NULL,

  -- One row per (account, kind of setting): a setting is one coherent object, not a bag
  -- of key/value pairs.
  CONSTRAINT account_settings_one_per_type UNIQUE (account_id, setting_type)
);

CREATE TRIGGER update_husonym_api_account_settings_updated_at
  BEFORE UPDATE ON husonym_api.account_settings
  FOR EACH ROW
  EXECUTE FUNCTION update_updated_at_column();

COMMENT ON TABLE husonym_api.account_settings
  IS 'Stores the settings of an account: a value that varies by account, part of which may be a secret, and that a human sets once';
