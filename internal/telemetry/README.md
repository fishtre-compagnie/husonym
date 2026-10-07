# Usage report of the instance

Once a day the instance prepares a JSON document that describes its usage: the usage report of
the instance. This package defines that document (`Report`), its published JSON Schema
(`schema/usage-report.v1.schema.json`, JSON Schema 2020-12) and the closed lists every value of
the document is taken from.

## What never enters the document

Every value of the report is one of:

- a number, a boolean or a date;
- a member of a closed list declared in this package (`lists.go`).

A value outside its list becomes `other`. Anything a user typed never enters the document: no host
name, schema, table, column, job, connection, account or user name, no fingerprint of one of them,
no query, no error message, and no counter per table, per database, per job or per account. Row
counts leave only as bands (`lt_1k` ... `gte_100m`).

Every input that is not a number passes through one of the functions of this package
(`ColumnTypeFamily`, `ConnectionType`, `TransformerName`, `InstallKind`, `RowsBucket`, ...) before
it is placed in a `Report`. The schema has `additionalProperties: false` at each level and an
`enum` for each closed list, so `Validate` refuses a document that carries anything else.

## What the counts mean

- `transformers.system[].columns` is the number of columns a transformer runs on. A column whose
  transformer is a PII text counts under that transformer and under each transformer it hands its
  findings to, so these counts can sum to more than `jobs.columns`.
- `users.users` counts people once; `users.by_role` counts a person once per account they are a
  member of, under the role held there, so the roles can sum to more than `users.users`.
- `unread` counts the jobs, the connections and the accounts the instance holds and could not
  read. A job or a connection that is not read is in no other count; an account that is not read
  is left out of the features in use and of the roles. The block is always there, with zeros when
  everything was read.

## Stable bytes

`Report.Marshal` sorts every array by its keys and writes no `null` array, so one state always
gives the same bytes; rows with the same keys are ordered by their count. Optional fields
(`diagnostics`, `duration_seconds`, `users.active_30d`, `auth_provider`, `postgres_major`,
`temporal_version`, `workers`) are absent, never zero. The identification block is never optional:
a report is only built with a license in force, so its five fields are always there.

## Seal

The report carries proof that its producer holds the license key of the instance, and the key
itself never appears in it.

- **Fingerprint**: `key_fingerprint` is the lowercase hex SHA-256 of the license key value (the
  base64 string as the instance holds it), surrounding whitespace trimmed.
- **Secret**: the key value is base64 of a JSON envelope whose `signature` field is base64 of the
  Ed25519 signature. The secret is `HKDF-SHA-256(ikm = signature bytes, salt = none, info =
  "husonym usage report seal v1", length = 32)`.
- **Seal**: the lowercase hex HMAC-SHA-256, keyed with that secret, of the exact bytes of the
  document as sent. Re-encoding the JSON changes the bytes and invalidates the seal.

`testdata/seal-vector.json` holds a throwaway key, a document, its fingerprint and its seal. It is
the reference for any other implementation. Refresh it with
`go test ./internal/telemetry -run Test_Seal_MatchesThePublishedVector -update`.

## Evolving the schema

`schema_version` is 1. A version only gains optional fields: nothing is renamed, removed or made
required, and every addition is announced in the release notes.

The `enum` of the schema are generated from the lists of this package (transformer names, gates,
features, roles, ...), so they are kept in one place. When a list grows (a new transformer in the
proto enum, a new feature or gate in `internal/license`), the test
`Test_Schema_IsUpToDateWithTheLists` fails until the schema file is refreshed:

```sh
go test ./internal/telemetry -run Test_Schema_IsUpToDateWithTheLists -update
```

Review and commit the diff of the schema file. A change of structure is made in the schema file by
hand, followed by the same command, which rewrites the file in its canonical form.
