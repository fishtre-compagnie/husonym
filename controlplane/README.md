# Control plane

A small service that receives the sealed daily reports posted by Husonym instances, checks each
one against the registry of issued licenses, and stores it in its own PostgreSQL database.

It is one binary, `husonym-controlplane`, and it imports nothing from the backend, the worker or
the CLI. It reuses `internal/license` (the keyring and the registry) and `internal/telemetry` (the
schema of a report and its seal).

## Commands

| Command                                                  | What it does                                                        |
| -------------------------------------------------------- | ------------------------------------------------------------------- |
| `husonym-controlplane migrate up`                        | Applies the pending migrations of the database (they are embedded). |
| `husonym-controlplane import-registry --registry <file>` | Loads the registry of issued licenses and prints three counts.      |
| `husonym-controlplane serve public`                      | Applies the migrations, then serves the public API.                 |

`import-registry` verifies every entry against the embedded keyring. An entry whose key does not
verify, whose id is not the one inside its key, or whose key names no customer is skipped and
counted; the others are imported, and the command exits non-zero. Reports that were waiting for a
license that has just been imported are checked in the same run. Should that last step fail, the
counts are printed all the same, the command exits non-zero, and the server takes it up again
within the hour.

## Environment

| Variable                    | Meaning                                           |
| --------------------------- | ------------------------------------------------- |
| `CONTROLPLANE_DATABASE_URL` | PostgreSQL connection string (required).          |
| `CONTROLPLANE_LISTEN_ADDR`  | Listen address of the server, `:8080` by default. |

The tables live in the `controlplane` schema. The table in which golang-migrate keeps the version
of the schema, `schema_migrations`, sits in `public`.

## Public API

- `POST /v1/usage-reports`: the body is the JSON document of a report, at most 128 KiB, sent with
  `Content-Type: application/json`. Two headers carry the seal and the fingerprint of the license
  key, `Husonym-Seal` and `Husonym-Key-Fingerprint`, each 64 lowercase hexadecimal characters.
- `GET /healthz`: answers 200.

Replies have no body.

| Status | When                                                                                           |
| ------ | ---------------------------------------------------------------------------------------------- |
| 204    | The report is stored, kept pending, already there, or in conflict with the one already stored. |
| 400    | The content type or a header is missing or malformed, or the report is refused: see below.     |
| 404    | Any other path.                                                                                |
| 405    | Another method than `POST` on the reports, or than `GET` on the health check.                  |
| 413    | The body is larger than the cap; a declared length over it is answered before it is read.      |
| 503    | A cap on the pending reports is reached, or the service failed. The sender tries again later.  |

A report is refused with 400 when:

- it is not a document of the schema;
- its day is more than one day ahead of the day it is received, or more than 60 days back;
- its seal is not the one of the document under the license of the fingerprint;
- the fingerprint in the header is not the one in the document;
- the license id it states is not the one of the license it is sealed under;
- its license was already seen on 50 instances in the last 60 days, and this instance is not one
  of them.

A stored report and a pending one answer the same. The other answers differ: a wrong seal, for
one, is only refused under a license that is known.

A report whose license is not in the registry yet is kept pending for 45 days (at most 100 per
fingerprint and 10000 in all). It is checked once the license is imported, as it would have been
on the day it was received, then stored or discarded. The first report stored for a license, an
instance and a day stays; a different one for the same three is counted as a conflict and dropped.

## What is stored

- For each license: its encoded key and what the key carries, the customer the key names (id and
  name), and from the registry the note and the fingerprint of the signing key.
- For each instance of a license: when its first and its last report were received, the day of its
  last report, the version of the product, and the kind of installation when the report tells it.
- For each report: its document exactly as received, its seal, when it was received, and how many
  different documents were sent again for the same license, instance and day, with the time of the
  last one. The same document sent again is not counted.
- For each license and day: how many reports were refused for their seal, and when the last was.
- The pending reports, as received: fingerprint, instance, day, document, seal and time of
  reception.

Nothing else about the caller is stored or logged: no address, no header other than the seal and
the fingerprint, and those two are stored, never logged.

## Logs

The line of a request is a path, a status and a fixed word for the outcome; a path that is not one
of the service is written `-`. When the service itself fails, the line carries the text of its own
error as well. The hourly maintenance writes one line with three counts (stored, discarded,
purged), or the text of its error. What `net/http` would log by itself is dropped.

## Tests

The integration tests start a PostgreSQL container and need Docker:

```sh
INTEGRATION_TESTS_ENABLED=1 go test ./controlplane/...
```

The generated code in `controlplane/gen/` is produced with sqlc from the queries in
`controlplane/sql/queries` and from the schema in `controlplane/migrations/sql`.

## Image

`docker build -f docker/Dockerfile.controlplane .` builds the image. Its entrypoint is
`/husonym-controlplane`, run as the non-root user 65532, and its default command is `serve public`.
