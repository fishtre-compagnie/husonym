CREATE TABLE IF NOT EXISTS husonym_api.license_keys (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),

  -- The signed key, exactly as it was given. It is what the worker is handed and what
  -- the verifier reads: nothing is derived from it that could drift away from it.
  key text NOT NULL,

  -- Read from the key when it was accepted, so that the table can be listed without
  -- decoding every row.
  license_id text NOT NULL,
  issued_at timestamptz NOT NULL,

  -- How the key reached the instance.
  origin text NOT NULL,
  CONSTRAINT license_keys_origin_known
    CHECK (origin IN ('interface', 'environment', 'file', 'renewal')),

  -- Rows are only ever inserted: the key in force is the latest one issued, so replacing
  -- a key is adding a row, and the older ones stay as history. Hence no updated_at.
  created_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,

  -- Null when no user did it (the environment, a file, a renewal), or once the user is gone.
  created_by_user_id uuid NULL,

  CONSTRAINT fk_license_keys_created_by_user
    FOREIGN KEY (created_by_user_id)
    REFERENCES husonym_api.users(id)
    ON DELETE SET NULL,

  -- The same key submitted twice is one row.
  CONSTRAINT license_keys_key_unique UNIQUE (key)
);

COMMENT ON TABLE husonym_api.license_keys
  IS 'Stores the license keys of the instance, not of an account: the one in force is the latest issued';
