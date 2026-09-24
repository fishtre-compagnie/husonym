-- What an API key may do, as entity:action permissions the RBAC already names (job:execute,
-- connection:view_sensitive...). A key can do nothing its permissions do not name: an empty
-- list allows nothing, never everything.
ALTER TABLE husonym_api.account_api_keys
  ADD COLUMN IF NOT EXISTS permissions text[] NOT NULL DEFAULT '{}';

-- The keys that exist could do everything; they keep that, written out rather than implied, so
-- that "everything" stays a scope someone can read and narrow.
UPDATE husonym_api.account_api_keys
SET permissions = ARRAY[
  'account:view', 'account:edit', 'account:create', 'account:delete',
  'connection:view', 'connection:view_sensitive', 'connection:create', 'connection:edit',
  'connection:delete',
  'job:view', 'job:create', 'job:edit', 'job:execute', 'job:delete'
];
