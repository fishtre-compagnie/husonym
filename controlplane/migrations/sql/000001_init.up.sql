CREATE SCHEMA IF NOT EXISTS controlplane;

CREATE TABLE controlplane.customers (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    external_id text NOT NULL UNIQUE,
    name text NOT NULL,
    note text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE controlplane.licenses (
    id text PRIMARY KEY,
    customer_id uuid NOT NULL REFERENCES controlplane.customers (id),
    encoded text NOT NULL UNIQUE,
    key_fingerprint text NOT NULL UNIQUE,
    kid text NOT NULL,
    plan text NOT NULL,
    features text[] NOT NULL,
    limits jsonb,
    telemetry text NOT NULL,
    issued_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    grace_days int,
    signing_key_fingerprint text NOT NULL,
    note text NOT NULL DEFAULT '',
    succeeds_license_id text REFERENCES controlplane.licenses (id),
    origin text NOT NULL CHECK (origin IN ('registry', 'console')),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE controlplane.instances (
    id text PRIMARY KEY,
    first_seen_at timestamptz NOT NULL,
    last_seen_at timestamptz NOT NULL,
    last_report_day date NOT NULL,
    last_license_id text NOT NULL REFERENCES controlplane.licenses (id),
    husonym_version text NOT NULL,
    install_kind text
);

-- The document is text, not jsonb: the seal is over the exact bytes received.
CREATE TABLE controlplane.usage_reports (
    instance_id text NOT NULL REFERENCES controlplane.instances (id),
    day date NOT NULL,
    license_id text NOT NULL REFERENCES controlplane.licenses (id),
    document text NOT NULL,
    seal text NOT NULL,
    received_at timestamptz NOT NULL,
    conflicts int NOT NULL DEFAULT 0,
    last_conflict_at timestamptz,
    PRIMARY KEY (instance_id, day)
);

CREATE TABLE controlplane.pending_reports (
    key_fingerprint text NOT NULL,
    instance_id text NOT NULL,
    day date NOT NULL,
    document text NOT NULL,
    seal text NOT NULL,
    received_at timestamptz NOT NULL,
    PRIMARY KEY (key_fingerprint, instance_id, day)
);

CREATE INDEX pending_reports_received_at_idx ON controlplane.pending_reports (received_at);

CREATE TABLE controlplane.seal_rejections (
    license_id text NOT NULL REFERENCES controlplane.licenses (id),
    day date NOT NULL,
    count int NOT NULL,
    last_at timestamptz NOT NULL,
    PRIMARY KEY (license_id, day)
);
