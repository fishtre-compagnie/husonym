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

- `runs` and `source_engines` hold the runs whose end the instance recorded on the day (UTC),
  whichever way it learned of it. A run that ended just before midnight and whose end was recorded
  just after is in the report of the next day, and in no other. `duration_seconds` is still the
  time from the start of a run to its end.
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
  "husonym usage report seal v1", length = 32)`. The signature is exactly 64 bytes: a key that
  carries one of another length, or none, is refused, since the secret derived from it would be
  anybody's to derive.
- **Seal**: the lowercase hex HMAC-SHA-256, keyed with that secret, of the exact bytes of the
  document as sealed. Re-encoding the JSON changes the bytes and invalidates the seal.
- **Check**: a seal is exactly 64 lowercase hex characters. Anything else is refused before any
  comparison, upper case included, so that a seal has one spelling. The seal is decoded and its 32
  bytes are compared in constant time with the HMAC computed again.

`testdata/seal-vector.json` holds a throwaway key, a document, its fingerprint and its seal. It is
the reference for any other implementation, and does not change: `-update` leaves it alone. Only
`go test ./internal/telemetry -run Test_Seal_MatchesThePublishedVector -update-seal-vector` mints
a new one, which every other implementation then has to follow.

## Sending

The sending is in `backend/internal/usagereport` (`sender.go`, `transport.go`). The mode in force
is computed by `telemetry.EffectiveMode`, from what the key provides and from `HUSONYM_TELEMETRY`,
which may only lower it; only the mode `online` sends.

- **Address**: `usagereport.DefaultReportURL`.
- **Request**: a `POST` whose body is the stored document, byte for byte, with three headers:
  `Content-Type` (`application/json`), `Husonym-Seal` (the seal of the document) and
  `Husonym-Key-Fingerprint` (the fingerprint of the key that sealed it).
- **Success**: any 2xx status. A redirect is not followed and counts as a failure; the body of
  an answer is read up to a limit and dropped.
- **First sending**: until an instance has sent a report since it started sending, a report
  leaves only 24 hours after it was prepared; once one was sent, the following ones go as soon
  as they are prepared. Leaving the mode `online`, or being without a license key in force,
  forgets since when it was sending, so coming back waits again on its first report. A replica
  claims no report once another one has recorded that the instance does not send.
- **Diagnostics switched off**: a report is sent as it is stored, so one that was prepared with
  the diagnostics is not sent while they are off: it stays, and reads as kept. Switching them
  back on makes it due again.
- **Retry**: a report that could not be sent is tried again after 6 hours, from the oldest, and
  reports of closed days older than 30 days are no longer sent. A failure is logged and never
  delays a request or a run.
- **Twice**: a report may arrive twice, when it left and could not be marked as sent. It is then
  the same report: same day, same bytes, same seal.

## The report for a period

On demand the instance makes a second document from its own tables: its usage report for a period
of months (`PeriodReport`, `schema/usage-period-report.v1.schema.json`). It follows the rule of
the report of a day: numbers, booleans, dates and members of the closed lists, a schema closed at
each level, and no counter per table, per database, per job or per account.

- A period is whole months, `from` to `to` included, written `YYYY-MM` and taken in UTC: 24 months
  at most, none of them in the future. That is as long as the instance keeps its reports of the
  day, so a period never reaches back to months it has forgotten. The month under way may be
  asked, and is told as far as it went.
- `identification` is the one of the license key in force when the document is made, with
  `days_to_expiry` counted from that moment.
- `runs` and `refusals` of a month are added up from the rows the instance keeps, not from the
  reports of the days, whose bands and percentiles do not add up: the runs whose end the instance
  recorded from the first instant of the month to the last before the next one, and the refusals
  of those days. The rows read leave as the band of their sum over the month, and
  `duration_seconds` holds the median and the 95th percentile over the month.
- `days_reported` is how many days of the month have a report of the day that is kept and still
  reads. `sources.count` is the highest count those reports hold, `version` the one of the last
  of them, and `state` the blocks of its diagnostic that tell a state (`installation`,
  `configuration`, `connections`, `jobs`, `transformers`, `column_types`, `features`, `users`,
  `unread`). The source versions and the errors of a day are not part of it.
- A month without such a report has `days_reported` 0 and no `sources`, `version` nor `state`.
  An absent block means that nothing is known, never that there was nothing: the instance
  prepared no report that month, or keeps it no longer, and a count of zero would be a statement
  it cannot make. `days_reported` is always there and tells how much of a month the three blocks
  stand on: a month the instance reported in part has them from the days it did report.
- A month whose last report was made with the diagnostic switched off has no `state`.
- With the diagnostic switched off, `runs`, `refusals` and `state` are absent from every month;
  `runs` and `refusals` are there together or not at all.

`PeriodReport.Marshal` gives stable bytes the way `Report.Marshal` does, with the months in
order, and `ValidatePeriod` checks a document against its schema. The document is sealed exactly
as the report of a day is: same fingerprint, same secret, same HMAC over the exact bytes.

The schema file of the period says of its own only how a period and a month are laid out. Every
block it shares with the report of a day, and every `enum` those blocks name, is copied from the
schema of the day by the command below, so that nothing is kept in two places.

## Evolving the schema

`schema_version` is 1. Within a version:

- a closed list may gain members, and the document may gain blocks;
- a field is never removed and never changes type.

The schema refuses what it does not know (`additionalProperties: false`, an `enum` per list), so a
document is validated with the schema of the revision that produced it, or of a later one: an
earlier schema refuses a member or a block it has not heard of. Every addition is announced in
the release notes.

The `enum` of the schema are generated from the lists of this package (transformer names, gates,
features, roles, ...), so they are kept in one place. When a list grows (a new transformer in the
proto enum, a new feature or gate in `internal/license`), the test
`Test_Schema_IsUpToDateWithTheLists` fails until the schema files are refreshed:

```sh
go test ./internal/telemetry -run Test_Schema_IsUpToDateWithTheLists -update
```

Review and commit the diff of the schema files. A change of structure is made in the schema file
of the day by hand, followed by the same command, which rewrites both files in their canonical
form and carries the change to the blocks the report for a period shares.
