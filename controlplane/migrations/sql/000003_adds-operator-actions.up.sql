-- The journal of what the operator does from the console. A line is written in the transaction
-- of the act it tells, and is never changed afterwards.
CREATE TABLE controlplane.operator_actions (
    id bigserial PRIMARY KEY,
    at timestamptz NOT NULL,
    operator text NOT NULL,
    action text NOT NULL CHECK (action IN (
        'customer_created', 'customer_updated', 'license_issued', 'license_renewed', 'license_key_shown'
    )),
    customer_id uuid REFERENCES controlplane.customers (id),
    license_id text REFERENCES controlplane.licenses (id),
    detail jsonb NOT NULL DEFAULT '{}'
);

CREATE INDEX operator_actions_at_idx ON controlplane.operator_actions (at DESC);

-- A license has one successor at most: of two renewals of the same license, the second is refused
-- here, whatever each of them read before writing.
CREATE UNIQUE INDEX licenses_succeeds_license_id_idx ON controlplane.licenses (succeeds_license_id)
WHERE succeeds_license_id IS NOT NULL;
