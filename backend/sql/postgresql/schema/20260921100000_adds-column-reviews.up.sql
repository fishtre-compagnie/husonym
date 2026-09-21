-- A column left out of a job's mappings is reported on every validation, because the job copies
-- it untransformed and nobody decided that it should. Some of them are fine that way, and
-- without somewhere to say so the list only ever grows: a list that never shrinks stops being
-- read, which is the failure the warning existed to prevent.
--
-- A row here is that decision. It is scoped to the job, not to the connection: the same column
-- may be fine to copy for a demo job and unacceptable for a sampling one.
CREATE TABLE husonym_api.column_reviews (
    account_id UUID NOT NULL,
    job_id UUID NOT NULL,
    table_schema TEXT NOT NULL,
    table_name TEXT NOT NULL,
    column_name TEXT NOT NULL,
    -- What the column was when it was reviewed. A decision is about a column as it stood, so
    -- these are compared on read: if the type or the detected category has moved since, the
    -- column was not what the reviewer looked at and it is reported again. Renaming a free-text
    -- field into something that holds addresses is exactly how a job quietly starts leaking.
    reviewed_data_type TEXT NOT NULL,
    reviewed_pii_category TEXT NOT NULL,
    -- Why it is acceptable. Optional, and the thing an auditor actually asks for.
    note TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by_id UUID NOT NULL,
    updated_by_id UUID NOT NULL,
    PRIMARY KEY (job_id, table_schema, table_name, column_name),
    FOREIGN KEY (account_id) REFERENCES husonym_api.accounts(id),
    -- The decision has no meaning without the job it was made for.
    FOREIGN KEY (job_id) REFERENCES husonym_api.jobs(id) ON DELETE CASCADE
);

CREATE INDEX idx_column_reviews_account_id ON husonym_api.column_reviews(account_id);

CREATE TRIGGER update_husonym_api_column_reviews_updated_at
BEFORE UPDATE ON husonym_api.column_reviews
FOR EACH ROW
EXECUTE FUNCTION update_updated_at_column();
