---
title: PII Detection Jobs
description: What a PII detection job reads in a source database, what it sends to the language model, and what its report holds
id: pii-detection-job
hide_title: false
slug: /guides/pii-detection-job
# cSpell:words IBAN IBANs Luhn SIRET SIREN prenom ville llama Ollama telefon apellido indirizzo woonplaats pesel senha nombre cliente customeremail dateofbirth Werkzeug PRÉNOM addressline lieunaissance passwordresettoken yescrypt staatsangehörigkeit staatsangehoerigkeit postcodenl addressbook streetview
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
3. For each table three independent detections answer, and their findings are kept **side
   by side**, without a merged verdict:
   - the **rules** (`regex` in the report);
   - the **language model** (`llm`), when the deployment configures one;
   - the **content analyzer** (`analyzer`), when the deployment has one, for the
     [free-text columns](#the-content-analyzer) the rules found nothing in.
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

1. **The name of the column**, read by the name rules of the
   [GDPR detection](/guides/detection-rgpd): the scan of a connection and this job answer
   from the same rules. They read the words of a name in English, French, German, Spanish,
   Italian, Dutch, Polish and Portuguese (`date_of_birth`, `prenom`, `telefon`, `apellido`,
   `indirizzo`, `woonplaats`, `pesel`, `senha`…):
   - A keyword is a word of the name, never letters inside a longer word: `mobile` is not
     read in `automobile`. Case and accents do not count: `PRÉNOM` reads `prenom`, and a
     German word is read with its umlaut or with the letters that stand for it
     (`staatsangehörigkeit`, `staatsangehoerigkeit`). A name written without separators is
     read as its words when the rules know them all (`customeremail`, `dateofbirth`,
     `addressline1`, `lieunaissance`, `passwordresettoken`); two last letters may follow a
     keyword of six letters or more (`postcodenl`). A name that holds a word the rules do
     not know is another word, and is not reported: `addressbook`, `streetview`,
     `pseudorandom`.
   - An income, a wage and a birth are reported as a person's (`annual_income`,
     `employee_income`, `hourly_wage`, `birth_date`). Beside a word of the accounts or of
     statistics they are not: `net_income`, `gross_income`, `income_tax`, `minimum_wage`,
     `birth_rate`. The word of a person beside them makes the name a finding again
     (`employee_net_income`). A salary is always reported.
   - `product_name` names a thing, `user_id` and `id_user` refer to another row, and a name
     that qualifies a datum without being one (`email_format`, `phone_type`,
     `address_count`, `is_email_verified`, `country_code`, `password_changed_at`) is not
     reported. A name that holds two data is reported for the one the qualifier belongs
     to: `address_zip_code` is a postal code.
   - A word that is ordinary in another language is a finding as the whole name, or beside
     a word of its own language: `nombre` and `nombre_cliente` are first names,
     `nombre_articles` is a count.
   - A rule does not apply to a column whose type cannot hold its datum: an integer column
     named `nombre` is not a first name, a timestamp column named `password` is not a
     password. A type is told by its whole name: a type defined in the schema (a domain,
     an enum) refuses no rule, whatever its name holds. A secret is also reported in a
     `uuid` column.
   - Passwords, tokens, keys and verification codes are reported, hashed or not
     (`password_hash`, `api_key`, `refresh_token`, `verification_code`).
2. **The format of the values**, when data sampling is enabled: when at least **half** of
   the non-blank values of a column pass a format check, the column is reported whatever
   its name. The checks are those of the GDPR detection: email address, IBAN (mod 97
   checksum), French social security number (mod 97 checksum), payment card (Luhn
   checksum and network prefix), IP address, French telephone number, civility, and
   password hash (bcrypt, Argon2, scrypt, yescrypt, PBKDF2 and the crypt schemes, also as
   Django, Werkzeug and LDAP directories store them, MySQL's own, and ASP.NET Identity
   version 3; version 2 of the latter carries no mark and is not recognized). A SIRET or a
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

On PostgreSQL and SQL Server, the rows of a table of more than 1000 rows are drawn from
pages taken at random over the whole table: about fifty pages, or the pages that hold
about 1000 rows when that is more; a table of fifty pages or fewer is read whole. On
MySQL and MariaDB, when the primary key is a single column of an integer type, they are
drawn from ten ranges of the key that follow one another from its lowest to its highest
value, up to 100 consecutive rows in each. In every other case — among them, on PostgreSQL and SQL Server, a table of 1000 rows or
fewer; a PostgreSQL table that was never analyzed, or a partitioned one none of whose
partitions was, or one of whose partitions is a foreign table; a view; a MySQL or MariaDB
table without such a primary key, whose key values span less than 1000, or whose ten
ranges hold fewer than 500 rows together — the rows are drawn from the first 1000 rows of the table, and whenever
the draw over the whole table fails or returns fewer rows than asked, the missing rows are
taken from those first 1000 rows, where a row already read can be read again. Two scans of
the same table can read different rows.

A layout is what a value looks like once its characters are replaced by their class. Each
run of characters of one class is written once: `A+` for uppercase letters, `a+` for other
letters, `9+` for digits, `?+` for the characters a layout does not show, one space for
spaces. Eleven punctuation characters (`@ . , - _ / : + ( ) #`) are written as they are,
where they stand between runs: `1985-03-12` has the layout `9+-9+-9+`. A layout is cut at
32 characters.

A profile holds no value, no letter and no digit of a value, no hash of a value, and no
smallest or largest value. The punctuation characters above, in a layout, are the only
characters of the values it may hold, under these conditions:

- a layout or a format enters a profile only when at least three rows have it;
- a layout in which every run is one character long is never in a profile: it would give
  the class of each character of a value;
- a value made of punctuation only is never its own layout: it is written `?+`;
- for a column with fewer than three values, the profile holds counts and the kind of the
  values, nothing else;
- for a column whose rows all hold the same value, the profile holds those and the format
  checks that value passes, which the rules read; the model is told the counts and the
  kind only.

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
values, which are neither recorded in the history of the run nor written in the logs.
Choose it only if the endpoint of the model may receive this data, for example a model
hosted inside your network.

A binary column never sends a value. It is known by its type in the catalogue of the
database, whatever the driver returns for it: `bytea` and the bit strings of PostgreSQL;
`binary`, `varbinary`, the `blob` family, `bit` and the spatial types of MySQL; `binary`,
`varbinary`, `image`, `rowversion` (and `timestamp`, its other name), `geography`,
`geometry`, `hierarchyid` and `sql_variant` of SQL Server, and the arrays of these types.

The catalogue lists a column of a domain under the name of the domain, not under its base
type: a domain over a binary type is told by its values. A value is not sent when it is
not valid UTF-8, when it holds a control character other than a tab or an end of line, or
when it is bytes as PostgreSQL writes them (`\x` and hexadecimal digits), alone or as the
elements of an array.

The model answers, for each column, one of the six categories or `none` (not personal
data), with a confidence from 0 to 1. The answer is checked: a column without a valid
answer is asked once more, then reported as unanswered. Only the answers whose confidence
reaches the threshold of the deployment (0.5 by default) are in the report.

What the model writes in a reasoning block is not read: `<think>`, `<thinking>`,
`<thought>`, `<thoughts>`, `<reasoning>`, `<reflection>`, `<scratchpad>`, `<seed:think>`
and `[THINK]`, in any case. Outside one, the answer is the last JSON object that names a
column, also inside a fenced block. An object inside another object is not an answer. A
column about which an object before the answer says something else, or whose key the
answer holds twice, is asked once more. When the endpoint reports that the completion did
not stop by itself (a `finish_reason` other than `stop`), the last object is the answer
only if nothing follows it but spaces or the end of a fenced block; otherwise every column
is asked once more. An answer is read up to one megabyte.

**What is recorded.** The sample values, the text of the request and the message of the
endpoint are never written in the logs of the worker nor recorded in the history of the
run. The history of the run holds what the model step is given and what it returns: the
name of the table, the names and the types of its columns, their profiles and the User
Prompt of the job; then the category and the confidence of each answer, which are also the
stored report.

When a request fails, what is kept of the endpoint's answer is its HTTP status and, for a
request without sample values, the type and the code of its error when they are short
identifiers. Its message, which may quote the request, is never kept; with sample values
the status alone is.

Beside the request itself, the client library adds headers that describe it: a
`User-Agent` with its version, and `X-Stainless-*` headers with the operating system, the
processor architecture, the Go version, the retry count and the timeout of the request.

## The content analyzer

Rules read names and formats, and the model is given statistics by default: personal data
buried in a sentence shows to neither. A column `comment` in which some values name a
customer passes for harmless. The **content analyzer** reads the values themselves. It is
the Presidio analyzer that the API already uses for the
[GDPR detection](/guides/detection-rgpd), and it is optional.

**Which columns.** The worker names the columns of text whose values average three words or
more, and in which the rules found nothing. It needs the profile of the columns, so the
analyzer is asked only when data sampling is enabled. The worker sends no value: it asks
the API to analyze the columns, 20 at a time, by their names.

**What leaves the database, and where it goes.** The API reads up to 50 filled values of
each of these columns from the source and sends them, one at a time, to the analyzer set by
`PRESIDIO_ANALYZER_URL`. The values go from the source to the API and from the API to the
analyzer, cut to their first 200 characters: a person named further in a long text is not
seen. They never reach the worker, nor the history of the run, nor a log of the worker. The
log of the API can quote a value when the analyzer refuses it. What comes back to the
worker is, per column, the category, the kind of entity that was recognized and two counts:
the values in which it was found and the values that were analyzed.

This holds for every job that samples data, **including one set to _Statistics only_**:
that setting is about what the model receives, and the analyzer is a separate service of
your deployment. There is no switch per job. To keep the values of these columns from the
analyzer, do not set `PRESIDIO_ANALYZER_URL`.

**What counts.** A column of free text is reported when the analyzer recognizes **a person**
in at least **two** of the values it analyzed. Places, numbers, references and companies
that sentences contain do not count. A column reported this way has the category
`free_text_pii`, a confidence _to review_, and the suggested transformer is
`TransformPiiText`. A column in which one entity covers a third of the values is reported as
it is by the GDPR detection, under that entity: a column of names, of cities, of telephone
numbers.

**Without an analyzer.** When the API has none, the first table that asks learns it, the
tables of the run that start afterwards do not ask, the stored report of the table says that
the analyzer was absent, and the run ends well.

**A column the analyzer refuses.** The analyzer may refuse the values of a column on every
run. The run ends well, the stored report of the table lists the columns that were not
analyzed and marks the analysis as partial, and the table is scanned again by the next
incremental run. An analyzer that is down while the API answers looks the same: check it
when no table of a run has an analyzer finding.

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

`PII_DETECT_LLM_URL` without the name of a model, `PII_DETECT_LLM_MODEL` with neither an
address nor a key, an address that holds a user, a password or a query, or a threshold
outside 0 to 1, stops the worker when it starts, with a message that names the setting.

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
- the rules of the worker: an upgrade of the worker that changes what the rules answer
  scans every table again;
- or when the earlier run could not ask the model, or the analyzer, about that table, or the
  analyzer refused some of its columns.

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
- **The analyzer cannot be asked** (the API cannot be reached): what the rules and the model
  found is kept and stored for the table, and the run ends failed as above. An API that has
  no analyzer is not this case, see [The content analyzer](#the-content-analyzer).
- **The analyzer refuses some columns**: the table is stored with what was found, the
  columns are listed in its report, and the run does not fail for it.
- **The run is canceled**: the tables being scanned are canceled and no index is stored.
  The reports of the tables that had completed stay readable.

To keep a table that always fails from failing every scheduled run, leave it out with the
_exclude_ **Table Scan Mode**.

## Upgrading

A deployment that runs PII detection jobs takes these steps when it upgrades:

1. Stop every worker before starting the new ones, or pause the PII detection jobs
   meanwhile: a run must not be shared between two versions of the worker.
2. The first incremental run after an upgrade scans every table.
3. A run with a table that failed, or that the model or the analyzer could not scan, ends
   failed, once its reports are stored.
4. A deployment that has an analyzer sends it the values of the free-text columns of the jobs
   that sample data, from the first run of the new version. A table that a run had already
   scanned past the model before the upgrade goes on as it was.
5. Before going back to a previous version of the API, set the jobs that send sample
   values back to _Statistics only_.

## Reading the report

The page of a run shows the **PII Detection Report**: one line per reported column, with
its table, the detections that reported it (`regex`, `llm`, `analyzer`), their categories,
the confidence of the model and, for the analyzer, in how many of the analyzed values the
entity was found. The export button gives the same lines as a CSV file.

A column that is not in the report was reported by no detection.
