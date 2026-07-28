DROP TRIGGER IF EXISTS update_husonym_api_slack_oauth_connections_updated_at ON husonym_api.slack_oauth_connections;

DROP TABLE IF EXISTS husonym_api.slack_oauth_connections;

DELETE FROM husonym_api.account_hooks WHERE hook_type = 'slack';

ALTER TABLE husonym_api.account_hooks
  DROP CONSTRAINT hook_type_not_null;

ALTER TABLE husonym_api.account_hooks
  DROP COLUMN hook_type;

ALTER TABLE husonym_api.account_hooks
  ADD COLUMN hook_type text GENERATED ALWAYS AS (
    CASE
      WHEN config->'webhook' IS NOT NULL THEN 'webhook'
      ELSE NULL
    END
  ) STORED;

ALTER TABLE husonym_api.account_hooks
  ADD CONSTRAINT hook_type_not_null CHECK (hook_type IS NOT NULL);

