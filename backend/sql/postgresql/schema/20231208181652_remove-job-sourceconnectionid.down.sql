ALTER TABLE husonym_api.jobs
ADD COLUMN IF NOT EXISTS connection_source_id uuid null;

ALTER TABLE husonym_api.jobs
DROP CONSTRAINT IF EXISTS fk_jobs_conn_source_id_conn_id;

ALTER TABLE husonym_api.jobs
ADD CONSTRAINT fk_jobs_conn_source_id_conn_id FOREIGN KEY (connection_source_id) REFERENCES husonym_api.connections(id);
