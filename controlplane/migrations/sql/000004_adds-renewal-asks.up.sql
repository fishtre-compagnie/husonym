-- What the instances of a license asked of its renewal: one row per license and instance, with
-- the last ask and the last license that was served to it. A row is only written for a request
-- whose seal was verified. As in the instances, the id is the one the instance gives itself.
CREATE TABLE controlplane.renewal_asks (
    license_id text NOT NULL REFERENCES controlplane.licenses (id),
    instance_id text NOT NULL,
    last_asked_at timestamptz NOT NULL,
    last_served_license_id text REFERENCES controlplane.licenses (id),
    last_served_at timestamptz,
    PRIMARY KEY (license_id, instance_id)
);
