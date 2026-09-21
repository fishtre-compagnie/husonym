-- What the runs changed in a job's mappings, for the jobs whose strategy for new columns is
-- anonymize_pending_review.
--
-- A run brings its job's mappings in step with the source: it maps the columns that appeared and
-- removes the mappings of those that disappeared. Under anonymize_pending_review it also records
-- each change here, with what it chose, and somebody reviews it: the anonymization of a job
-- should not move without anyone knowing. A row is pending until it is reviewed — or, for an
-- added column, until somebody changes the mapping the run chose, which is a decision too.
CREATE TABLE husonym_api.job_mapping_changes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id UUID NOT NULL,
    job_id UUID NOT NULL,
    -- The run that made the change
    job_run_id TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    table_schema TEXT NOT NULL,
    table_name TEXT NOT NULL,
    column_name TEXT NOT NULL,
    -- added: the source gained the column, and the run mapped it.
    -- removed: the source lost it, and the run removed its mapping.
    -- type_changed: the column's type is not the one the previous run saw.
    kind TEXT NOT NULL CHECK (kind IN ('added', 'removed', 'type_changed')),
    -- The transformer the run chose (added), or the one the removed mapping had (removed)
    transformer JSONB,
    -- The column's type as the run saw it, and the one the previous run saw (type_changed)
    data_type TEXT NOT NULL DEFAULT '',
    previous_data_type TEXT NOT NULL DEFAULT '',
    -- What the column looks like to the PII detection, by name and type, when the change was made
    pii_category TEXT NOT NULL DEFAULT '',
    reviewed_at TIMESTAMP,
    reviewed_by_id UUID,
    -- Why the change is fine. Optional, and the thing an auditor actually asks for.
    note TEXT,
    FOREIGN KEY (account_id) REFERENCES husonym_api.accounts(id),
    FOREIGN KEY (job_id) REFERENCES husonym_api.jobs(id) ON DELETE CASCADE
);

-- The bell reads the pending changes of every job of an account at once.
CREATE INDEX idx_job_mapping_changes_pending
    ON husonym_api.job_mapping_changes(account_id, job_id)
    WHERE reviewed_at IS NULL;

-- The columns of a job's source as its last run saw them, types included: the only way to tell,
-- on the next run, that a column changed type. Written by every run, whatever the strategy.
CREATE TABLE husonym_api.job_source_columns (
    job_id UUID NOT NULL,
    table_schema TEXT NOT NULL,
    table_name TEXT NOT NULL,
    column_name TEXT NOT NULL,
    data_type TEXT NOT NULL,
    PRIMARY KEY (job_id, table_schema, table_name, column_name),
    FOREIGN KEY (job_id) REFERENCES husonym_api.jobs(id) ON DELETE CASCADE
);
