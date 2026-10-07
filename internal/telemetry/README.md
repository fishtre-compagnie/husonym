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

## Stable bytes

`Report.Marshal` sorts every array by its keys and writes no `null` array, so one state always
gives the same bytes. Optional fields (`diagnostics`, `duration_seconds`, `users.active_30d`,
`auth_provider`, `postgres_major`, `temporal_version`, `workers`, and the license values when no
key was read) are absent, never zero.

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
