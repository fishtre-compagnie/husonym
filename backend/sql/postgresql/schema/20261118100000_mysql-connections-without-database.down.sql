-- Puts back the databases the up migration cleared, on the connections that still name none.
UPDATE husonym_api.connections AS c
SET connection_config = jsonb_set(
  c.connection_config,
  '{mysqlConfig,connection,name}',
  to_jsonb(d.database_name)
)
FROM husonym_api.mysql_connection_databases_cleared AS d
WHERE c.id = d.connection_id
  AND c.connection_config #> '{mysqlConfig,connection}' IS NOT NULL
  AND coalesce(c.connection_config #>> '{mysqlConfig,connection,name}', '') = '';

DROP TABLE IF EXISTS husonym_api.mysql_connection_databases_cleared;
