-- What an API key may do, as entity:action permissions the RBAC already names (job:execute,
-- connection:view_sensitive...). A key can do nothing its permissions do not name: an empty
-- list allows nothing, never everything.
--
-- The default is every permission, and it is what the keys that exist receive: they could do
-- everything, and keep that, written out rather than implied, so that "everything" stays a
-- scope someone can read and narrow. It also covers the code that predates permissions —
-- during a rolling deploy, or after a rollback of the code alone — which creates keys without
-- naming any. The code that knows permissions always writes the list, an empty one included,
-- so the default never speaks for it.
ALTER TABLE husonym_api.account_api_keys
  ADD COLUMN IF NOT EXISTS permissions text[] NOT NULL DEFAULT ARRAY[
    'account:view', 'account:edit', 'account:create', 'account:delete',
    'connection:view', 'connection:view_sensitive', 'connection:create', 'connection:edit',
    'connection:delete',
    'job:view', 'job:create', 'job:edit', 'job:execute', 'job:delete'
  ];
