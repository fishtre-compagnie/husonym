-- The database of a MySQL connection entered by host turns from a required field the jobs
-- ignored into an optional scope: when set, the jobs are offered the tables of that database
-- only. Every connection made so far had to name one without it meaning anything, so it is
-- cleared, and each keeps offering every database its account reaches, as before. The names
-- are kept aside so the down migration can put them back.
CREATE TABLE IF NOT EXISTS husonym_api.mysql_connection_databases_cleared (
  connection_id uuid PRIMARY KEY,
  database_name text NOT NULL
);

INSERT INTO husonym_api.mysql_connection_databases_cleared (connection_id, database_name)
SELECT id, connection_config #>> '{mysqlConfig,connection,name}'
FROM husonym_api.connections
WHERE coalesce(connection_config #>> '{mysqlConfig,connection,name}', '') <> ''
ON CONFLICT (connection_id) DO NOTHING;

UPDATE husonym_api.connections
SET connection_config = connection_config #- '{mysqlConfig,connection,name}'
WHERE coalesce(connection_config #>> '{mysqlConfig,connection,name}', '') <> '';
