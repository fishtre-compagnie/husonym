---
title: PII Detection Jobs
description: What a PII detection job reads in a source database, what it sends to the language model, and what its report holds
id: pii-detection-job
hide_title: false
slug: /guides/pii-detection-job
# cSpell:words IBAN IBANs Luhn SIRET SIREN prenom ville llama Ollama
---

## Introduction

A **PII Detection** job goes through the tables of a source connection and says, column by
column, which ones hold personal data. It has no destination and writes nothing to the
source: its only product is a report, shown on the page of the run.

It is not the [GDPR detection](/guides/detection-rgpd) of the mapping table, which helps
choosing the transformers of a sync job. The detection job runs on the worker, can be
scheduled, and covers a whole database.

Supported sources: **PostgreSQL, MySQL and SQL Server**, and a data generation job whose
foreign keys come from one of these. For any other source (MongoDB, DynamoDB, object
storage, generation by a language model) the run fails at its first step, with a message
that names the kind of the source.

## What a run does

1. It lists the tables of the source, filtered by the **Table Scan Mode** and **Patterns**
   settings.
2. It scans each table in a child workflow, **three tables at once** by default.
3. For each table two independent detections answer, and their findings are kept **side
   by side**, without a merged verdict:
   - the **rules** (`regex` in the report);
   - the **language model** (`llm`), when the deployment configures one.
4. The report of each table is stored as soon as it is known: the page of the run shows it
   while the run goes on.
5. At its end, the run stores the index of its reports.

## Categories

| Category         | What it covers                                                                  |
| ---------------- | ------------------------------------------------------------------------------- |
| `national_id`    | An identifier issued by an authority: social security number, passport, license |
| `contact`        | A way to reach a person: email address, telephone number                        |
| `financial`      | Payment card, bank account, IBAN, salary                                        |
| `personal`       | What describes a person: first name, last name, date of birth, age, gender      |
| `location`       | Postal address, city, postal code, country, IP address                          |
| `authentication` | A password or its hash, a secret, a token                                       |

## The rules

The rules call no service. The first one that answers wins:

1. **The name of the column.** Whole names (`email`, `date_of_birth`,
   `credit_card_number`, `password`…) and the dictionary of the
   [GDPR detection](/guides/detection-rgpd), which reads English, French, German, Spanish,
   Italian, Dutch, Polish and Portuguese (`prenom`, `telefon`, `apellido`, `indirizzo`,
   `woonplaats`, `pesel`, `senha`…), with its exclusions: `product_name` names a thing,
   `user_id` refers to another row, and a name that qualifies a datum without being one
   (`email_format`, `phone_type`, `address_count`, `is_email_verified`, `country_code`) is
   not reported. Passwords, tokens and keys are reported, hashed or not (`password_hash`,
   `api_key`, `refresh_token`).
2. **The format of the values**, when data sampling is enabled: when at least **half** of
   the non-blank values of a column pass a format check, the column is reported whatever
   its name. The checks are those of the GDPR detection: email address, IBAN (mod 97
   checksum), French social security number (mod 97 checksum), payment card (Luhn
   checksum and network prefix), IP address, French telephone number, civility, and
   password hash (bcrypt, Argon2, scrypt, PBKDF2 and the crypt schemes). A SIRET or a
   SIREN identifies a company: it makes no finding of the rules, and is passed to the
   model as evidence.

A name that says personal data is not withdrawn because the values pass no check: a check
that stays silent proves nothing.

**Rules alone do not find personal data in a column with a neutral name**, unless its
values have one of the formats above: a column `c17` of first names, or a free text about
a person, is found by the model only. A job that runs without a model ends well and says
so: the stored report of each table names the detections it rests on (`sources`: `rules`,
or `rules` and `model`).

## Data sampling

The **Data Sampling** setting lets the job read rows. When it is enabled, the worker reads
**up to 200 rows per table**, inside your deployment, and reduces each column to a
**profile**: how many values are null and how many are distinct, their lengths, the shares
of letters and of digits, the most frequent layouts (`a+.a+@a+.a+` for an email address),
and the share of the values that pass each format check. The rows do not leave the step
that read them.

A profile holds no value, no part of a value, no hash of a value, and no smallest or
largest value. A layout or a format enters a profile only when at least three rows have
it, and a value made of punctuation only is never its own layout. For a column with fewer
than three values the profile holds counts and the kind of the values, nothing else.

When data sampling is disabled no row is read: only the names of the tables, the names of
the columns and their types are used.

An empty table, or a table whose sample could not be read within 30 seconds, is scanned on
its names and types.

## What the model receives

The **What the model receives** setting shows only when data sampling is enabled.

| Choice                           | What is sent to the model, for each column                                                                      |
| -------------------------------- | --------------------------------------------------------------------------------------------------------------- |
| Data sampling disabled           | The name, the SQL type, whether it is nullable                                                                  |
| **Statistics only** (default)    | The name, the SQL type, whether it is nullable, and the profile. No value.                                      |
| **Statistics and sample values** | In addition: at most **5 distinct values**, none null, each cut to **64 characters**. Never of a binary column. |

In every case the request also carries the name of the table and the **User Prompt** of
the job, cut to 2,000 characters. It goes to the endpoint configured on the worker, and
nowhere else.

With _Statistics and sample values_, the table is read a second time to take these
values, which are written neither in the history of the run nor in the logs. Choose it
only if the endpoint of the model may receive this data, for example a model hosted inside
your network.

A binary column never sends a value. It is known by its type in the catalogue of the
database, whatever the driver returns for it: `bytea` and the bit strings of PostgreSQL;
`binary`, `varbinary`, the `blob` family, `bit` and the spatial types of MySQL; `binary`,
`varbinary`, `image`, `rowversion` (and `timestamp`, its other name), `geography`,
`geometry` and `hierarchyid` of SQL Server. A text that is not valid UTF-8 is not sent
either.

The model answers, for each column, one of the six categories or `none` (not personal
data), with a confidence from 0 to 1. The answer is checked: a column without a valid
answer is asked once more, then reported as unanswered. Only the answers whose confidence
reaches the threshold of the deployment (0.5 by default) are in the report. An answer is
read for the JSON object it holds, also after a reasoning block or inside a fenced block,
and up to one megabyte.

Nothing of what is sent to the model, and nothing of what it answers, is written in the
logs of the worker or in the history of the run. When a request fails, what is kept of
the endpoint's answer is its HTTP status and the type and the code of its error, when
they are short identifiers: never its message, which may quote the request.

Beside the request itself, the client library adds headers that describe it: a
`User-Agent` with its version, and `X-Stainless-*` headers with the operating system, the
processor architecture, the Go version, the retry count and the timeout of the request.

## Configuring the model

The model is set on the **worker**, through
[environment variables](/deploy/environment-variables#worker):

| Variable                           | Role                                                   |
| ---------------------------------- | ------------------------------------------------------ |
| `PII_DETECT_LLM_MODEL`             | The name of the model                                  |
| `PII_DETECT_LLM_URL`               | An endpoint that speaks the OpenAI chat completion API |
| `PII_DETECT_LLM_API_KEY`           | The key of the endpoint, when it asks for one          |
| `PII_DETECT_LLM_MIN_CONFIDENCE`    | The confidence threshold, from 0 to 1 (0.5 by default) |
| `TABLE_PII_DETECT_MAX_CONCURRENCY` | How many tables a run scans at once (3 by default)     |

A **local model** is declared by its address and its name, without a key:

```bash
PII_DETECT_LLM_URL=http://llama:8080/v1
PII_DETECT_LLM_MODEL=<the name of the model that is served>
```

Nothing then leaves your network, whatever the setting of the job. The endpoint must
accept an answer bound by a JSON schema (`response_format` of type `json_schema`) and a
temperature of 0, which llama.cpp, vLLM and Ollama offer.

**Without a configured model** the job runs on the rules alone, and its report holds
`regex` findings only.

The worker reads these settings and no other for the model:

- The address is `PII_DETECT_LLM_URL`, else `OPENAI_BASE_URL`, else the OpenAI API.
- The key is `PII_DETECT_LLM_API_KEY`. `OPENAI_API_KEY` stands in for it only when the
  address is not `PII_DETECT_LLM_URL`: the key of an OpenAI account is never sent to the
  endpoint that setting names. To ask such an endpoint without a key, set none.
- `OPENAI_ORG_ID` and `OPENAI_PROJECT_ID` are sent to the OpenAI API only.
- A deployment that only sets `OPENAI_API_KEY` asks `gpt-4o-mini` at the OpenAI API.
- `OPENAI_BASE_URL` alone, without a key and without `PII_DETECT_LLM_MODEL`, configures
  no model: the worker says so when it starts.

`PII_DETECT_LLM_URL` without the name of a model, an address that holds a user, a password
or a query, or a threshold outside 0 to 1, stops the worker when it starts, with a message
that says so.

When it starts, the worker writes in its log where the requests go:
`PII detection asks the model <name> at <host>`, or
`PII detection runs without a model`.

## Incremental scans

With **Incremental Scans** enabled, a run starts from the latest run of the job that stored
a report, and scans again only the tables whose way of being scanned changed. A table is
scanned again when one of these changes:

- its columns, or the type of one of them;
- the data sampling setting, what the model receives, or the User Prompt of the job;
- the model configured on the worker;
- or when the earlier run could not ask the model about that table.

Unchanged tables keep their earlier report. A table that was dropped from the source, or
that the filter now leaves out, leaves the report. When the earlier run cannot be found,
every table is scanned.

## When something fails

- **A table cannot be scanned** (its columns cannot be read, it takes too long): the other
  tables go on. The run stores the reports it has, then ends **failed**, with a message
  that names the tables: a run that ends well means that every table was scanned. The _job
  run failed_ event is sent to the [account hooks](/guides/account-hooks).
- **The model cannot be asked** (the endpoint is unreachable, the key is refused): what
  the rules found is kept and stored for the table, and the run ends failed as above. A
  refused key or request is not attempted again.
- **The model does not answer for some columns**: up to half of the columns of a table,
  the table is complete, the columns are listed in its stored report, and the run does
  not fail for it. Past half, the model did not scan the table: it counts as a model that
  cannot be asked.
- **The run is canceled**: the tables being scanned are canceled and no index is stored.
  The reports of the tables that had completed stay readable.

To keep a table that always fails from failing every scheduled run, leave it out with the
_exclude_ **Table Scan Mode**.

## Upgrading

A deployment that already runs PII detection jobs takes these steps when it upgrades to
this version:

1. Stop the workers of the earlier version, or pause the PII detection jobs, before
   starting the new workers. A worker of the earlier version sends whole values to the
   model for a job with data sampling, and cannot continue a run that tolerated a model
   failure, that failed for an incomplete table, or that scanned two tables of the same
   sanitized name.
2. Expect the first incremental run to scan every table again: what identifies an
   unchanged table now includes the types of its columns and the settings of the job.
3. Expect a run with a table that failed, or that the model could not scan, to end
   failed, once its reports are stored.
4. Before rolling the API back to the earlier version, set the jobs that send sample
   values back to _Statistics only_: the earlier API cannot read a job that holds that
   choice.

## Reading the report

The page of a run shows the **PII Detection Report**: one line per reported column, with
its table, the detections that reported it (`regex`, `llm`), their categories and the
confidence of the model. The export button gives the same lines as a CSV file.

A column that is not in the report was reported by neither detection.
