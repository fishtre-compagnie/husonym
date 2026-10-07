---
title: Usage report
description: What the usage report of a Husonym instance contains, where it goes, and how to read, reduce or stop it
id: usage-report
hide_title: false
slug: /deploy/usage-report
---

Each day, a Husonym instance prepares a report of its own usage. When its license
provides for it, the instance sends that report to Husonym. The report is made of
counts and of values taken from fixed lists.

## What it contains

- Which license key and which instance the report is about.
- The version of Husonym.
- The number of source databases.
- Unless you turn them off, diagnostics: how the instance is installed, the number
  of connections by type, of jobs by kind, of columns by built-in transformer and
  by family of data type, the features in use, the runs of the day by status with
  their durations, the number of rows read as a range ("under 10 million"), and
  the number of users by role.

## What it never contains

- A name: host, database, schema, table, column, job, connection, account or user.
- A query, a filter, or the code of a transformer.
- An error message.
- A count per table, per job or per account.
- Any of your data.

## Reading it before it leaves

**Settings → License → Usage report** shows the report of each day exactly as it
is sent, and what became of the last thirty. The first report of an instance
waits 24 hours before it is sent.

## Where it goes

To `https://license.husonym.com`, over HTTPS. The instance honors `HTTPS_PROXY`.
When a report cannot be sent, the instance tries again later and nothing else is
affected: no job is delayed or refused.

## Reducing or stopping it

| Variable | Effect |
| --- | --- |
| `HUSONYM_TELEMETRY_DIAGNOSTICS=false` | Leaves the diagnostics out of the report. |
| `HUSONYM_TELEMETRY=offline` | Sends nothing. You hand over a report file instead. |
| `HUSONYM_TELEMETRY=off` | Sends nothing. |

With the Helm chart, set `usageReport.mode` and `usageReport.diagnostics` instead
(`api.usageReport.mode` and `api.usageReport.diagnostics` in the `husonym` chart).

Your license says what reporting it provides for. When the instance is set below
that, the License page says so. Nothing is blocked.

## The report file

For an instance without outbound access:

    husonym usage-report --from 2026-01 --to 2026-12 --output report.json

A period is 24 months at most. See [`husonym usage-report`](../cli/usage-report.md).

The file holds the same counts, by month. You can read it before you hand it over.
