# Control plane

A small service that receives the sealed daily reports posted by Husonym instances, checks each
one against the registry of issued licenses, and stores it in its own PostgreSQL database.

It is one binary, `husonym-controlplane`, and it imports nothing from the backend, the worker or
the CLI. It reuses `internal/license` (the keyring and the registry) and `internal/telemetry` (the
schema of a report and its seal).

## Commands

| Command                                         | What it does                                                           |
| ----------------------------------------------- | ---------------------------------------------------------------------- |
| `husonym-controlplane migrate up`               | Applies the pending migrations of the database (they are embedded).    |
| `husonym-controlplane import-registry --registry <file>` | Loads the registry of issued licenses and prints three counts. |
| `husonym-controlplane serve public`             | Applies the migrations, then serves the public API.                    |

`import-registry` verifies every entry against the embedded keyring. An entry that does not verify
is skipped and counted, the others are imported, and the command exits non-zero. Reports that
were waiting for a license that has just been imported are stored in the same run.

## Environment

| Variable                    | Meaning                                  |
| --------------------------- | ---------------------------------------- |
| `CONTROLPLANE_DATABASE_URL` | PostgreSQL connection string (required). |
| `CONTROLPLANE_LISTEN_ADDR`  | Listen address of the server, `:8080` by default. |

The tables live in the `controlplane` schema.

## Public API

- `POST /v1/usage-reports`: the body is the JSON document of a report, at most 128 KiB. Two headers
  carry the seal and the fingerprint of the license key, `Husonym-Seal` and
  `Husonym-Key-Fingerprint`, each 64 lowercase hexadecimal characters.
- `GET /healthz`: answers 200.

Replies have no body.

| Status | When                                                                                                   |
| ------ | ------------------------------------------------------------------------------------------------------ |
| 204    | The report is stored, kept pending, already there, or in conflict with the one already stored.         |
| 400    | The report is malformed, outside the schema, too far from today (more than a day ahead, more than 60 days back), or its seal is wrong. |
| 413    | The body is larger than the cap.                                                                       |
| 503    | A cap on the pending reports is reached, or the service failed. The sender tries again later.          |

A reply never tells whether a license is known: a stored report and a pending one are answered
alike. A report whose license is not in the registry yet is kept pending for 45 days (at most 100
per fingerprint and 10000 in all) and is checked and stored once the license is imported. The
first report stored for an instance and day stays; a different one for the same instance and day
is counted as a conflict and dropped.

## What is stored

For each report: its document exactly as received, its seal, the day and the instance it
belongs to, and the moment it was received. For each license: what its key carries. Counters of
repeats, conflicts and rejected seals are kept per license.

Nothing else about the caller is stored or logged: no address, no header other than the seal and
the fingerprint. A log line is made of a path, a status and a fixed word.

## Tests

The integration tests start a PostgreSQL container and need Docker:

```sh
INTEGRATION_TESTS_ENABLED=1 go test ./controlplane/...
```

The generated code in `controlplane/gen/` is produced with sqlc from `controlplane/sql/`.

## Image

`docker build -f docker/Dockerfile.controlplane .` builds the image. Its entrypoint is
`/husonym-controlplane`, run as a non-root user, and its default command is `serve public`.
