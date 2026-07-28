-- this will only work if there are not-null source connection ids
ALTER TABLE husonym_api.jobs
ALTER COLUMN connection_source_id SET NOT NULL;
