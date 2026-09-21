-- The columns a job's last run copied untransformed because they were not in its mappings,
-- recorded by the run itself.
--
-- It is a fact about a run, not a to-do: what is pending is derived on read, by taking away the
-- columns whose passthrough somebody has accepted (column_reviews). Keeping the two apart is what
-- lets a decision be withdrawn, or be invalidated by the column changing, without the run having
-- to know about decisions at all.
--
-- Recorded by the run rather than computed on display, because computing it means introspecting
-- the source of every job, and a page anybody opens on logging in cannot afford to do that to
-- production databases. The price is that it is as fresh as the last run — which is the moment a
-- new column starts leaking, so it is the right one.
CREATE TABLE husonym_api.unmapped_passthroughs (
    account_id UUID NOT NULL,
    job_id UUID NOT NULL,
    table_schema TEXT NOT NULL,
    table_name TEXT NOT NULL,
    column_name TEXT NOT NULL,
    -- The type the run saw. An accepted decision is checked against it.
    data_type TEXT NOT NULL,
    -- When a run first copied this column untransformed: how long it has been going on is the
    -- first thing anyone asks once they find out.
    first_seen_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    -- The run that last saw it. A run replaces the job's set by removing the rows it did not see,
    -- which is how a column that has since been mapped leaves the list.
    last_seen_job_run_id TEXT NOT NULL,
    PRIMARY KEY (job_id, table_schema, table_name, column_name),
    FOREIGN KEY (account_id) REFERENCES husonym_api.accounts(id),
    FOREIGN KEY (job_id) REFERENCES husonym_api.jobs(id) ON DELETE CASCADE
);

-- The bell reads every job of an account at once.
CREATE INDEX idx_unmapped_passthroughs_account_id ON husonym_api.unmapped_passthroughs(account_id);
