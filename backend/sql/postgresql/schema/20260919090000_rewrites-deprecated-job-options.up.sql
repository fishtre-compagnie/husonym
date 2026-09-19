-- The deprecated options are gone from the API and from the models that read the saved
-- jobs. Jobs saved with them are rewritten into the strategies that replaced them, the
-- way reading them did: a strategy already saved wins.

-- Sources: halting on a new column is the halt strategy.
UPDATE husonym_api.jobs
SET connection_options = jsonb_set(connection_options, '{postgresOptions,newColumnAdditionStrategy}', '{"haltJob": {}}')
WHERE (connection_options #>> '{postgresOptions,haltOnNewColumnAddition}')::boolean
  AND connection_options #> '{postgresOptions,newColumnAdditionStrategy}' IS NULL;

UPDATE husonym_api.jobs
SET connection_options = jsonb_set(connection_options, '{mysqlOptions,newColumnAdditionStrategy}', '{"haltJob": {}}')
WHERE (connection_options #>> '{mysqlOptions,haltOnNewColumnAddition}')::boolean
  AND connection_options #> '{mysqlOptions,newColumnAdditionStrategy}' IS NULL;

UPDATE husonym_api.jobs
SET connection_options = jsonb_set(connection_options, '{mssqlOptions,newColumnAdditionStrategy}', '{"haltJob": {}}')
WHERE (connection_options #>> '{mssqlOptions,haltOnNewColumnAddition}')::boolean
  AND connection_options #> '{mssqlOptions,newColumnAdditionStrategy}' IS NULL;

UPDATE husonym_api.jobs
SET connection_options = connection_options
  #- '{postgresOptions,haltOnNewColumnAddition}'
  #- '{mysqlOptions,haltOnNewColumnAddition}'
  #- '{mssqlOptions,haltOnNewColumnAddition}'
WHERE connection_options -> 'postgresOptions' ? 'haltOnNewColumnAddition'
   OR connection_options -> 'mysqlOptions' ? 'haltOnNewColumnAddition'
   OR connection_options -> 'mssqlOptions' ? 'haltOnNewColumnAddition';

-- Destinations: doing nothing on a conflict is the nothing strategy. SQL Server keeps its
-- doNothing, which is not deprecated.
UPDATE husonym_api.job_destination_connection_associations
SET options = jsonb_set(options, '{postgresOptions,onConflictConfig,onConflictStrategy}', '{"doNothing": {}}')
WHERE (options #>> '{postgresOptions,onConflictConfig,doNothing}')::boolean
  AND options #> '{postgresOptions,onConflictConfig,onConflictStrategy}' IS NULL;

UPDATE husonym_api.job_destination_connection_associations
SET options = jsonb_set(options, '{mysqlOptions,onConflict,onConflictStrategy}', '{"doNothing": {}}')
WHERE (options #>> '{mysqlOptions,onConflict,doNothing}')::boolean
  AND options #> '{mysqlOptions,onConflict,onConflictStrategy}' IS NULL;

UPDATE husonym_api.job_destination_connection_associations
SET options = options
  #- '{postgresOptions,onConflictConfig,doNothing}'
  #- '{mysqlOptions,onConflict,doNothing}'
WHERE options #> '{postgresOptions,onConflictConfig}' ? 'doNothing'
   OR options #> '{mysqlOptions,onConflict}' ? 'doNothing';
