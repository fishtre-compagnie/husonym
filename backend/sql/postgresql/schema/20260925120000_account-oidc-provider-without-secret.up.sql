-- An account's identity provider is a public client: the secret a setting could hold for it
-- was never used, and the field is gone from the contract. What was stored is removed, so
-- that no encrypted secret is left behind for nothing.
UPDATE husonym_api.account_settings
SET config = config #- '{oidcProvider,clientSecret}'
WHERE config->'oidcProvider' ? 'clientSecret';
