-- The last content scan of each table of a connection. Without it the scan ran again each
-- time the mapping of a job was opened, one sample per table read from the source database
-- and sent to the analyzer, to find what the previous visit had already found.
--
-- A result holds no value read from the table: the verdicts and detections carry categories,
-- counts and column names, which the mappings of the jobs already hold.
CREATE TABLE IF NOT EXISTS husonym_api.pii_scan_results (
  connection_id uuid NOT NULL,
  schema_name text NOT NULL,
  table_name text NOT NULL,

  -- The columns of the table and their types when it was scanned, as
  -- [{"column": ..., "dataType": ...}]. A table that has other columns now has changed
  -- since, and is to be scanned again.
  columns jsonb NOT NULL,

  -- The DetectPiiInConnectionDataResponse of the scan, marshaled by protojson.
  result jsonb NOT NULL,

  scanned_at timestamptz NOT NULL DEFAULT CURRENT_TIMESTAMP,

  CONSTRAINT pii_scan_results_pkey PRIMARY KEY (connection_id, schema_name, table_name),

  CONSTRAINT fk_pii_scan_results_connection
    FOREIGN KEY (connection_id)
    REFERENCES husonym_api.connections(id)
    ON DELETE CASCADE
);

COMMENT ON TABLE husonym_api.pii_scan_results
  IS 'Stores the last content scan of each table of a connection, and the columns the table had then';

-- A connection whose settings change may now reach another server or another database:
-- what its tables were found to hold no longer says anything about the ones it reaches.
CREATE OR REPLACE FUNCTION husonym_api.forget_pii_scan_results()
RETURNS TRIGGER AS $$
BEGIN
  DELETE FROM husonym_api.pii_scan_results WHERE connection_id = NEW.id;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER forget_pii_scan_results_on_connection_config
  AFTER UPDATE OF connection_config ON husonym_api.connections
  FOR EACH ROW
  WHEN (OLD.connection_config IS DISTINCT FROM NEW.connection_config)
  EXECUTE FUNCTION husonym_api.forget_pii_scan_results();
