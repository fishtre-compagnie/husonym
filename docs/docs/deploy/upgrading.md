---
title: Upgrading
description: What to do, and what to expect, when upgrading a Husonym deployment
id: upgrading
hide_title: false
slug: /deploy/upgrading
---

## The general procedure

1. Read the section below that matches the version you are leaving.
2. Upgrade the API first, with its database migrations. See
   [Husonym Migrations](/deploy/database#husonym-migrations).
3. Replace every worker in one rollout. A run must not be shared between two versions of
   the worker: stop the previous workers before the new ones take runs, or pause the
   schedules meanwhile.
4. Upgrade the web app.

Do not go back to a previous version of the worker while runs are in progress.

## Upgrading from v0.2

### Workers

Follow step 3 above strictly. A run that a new worker has taken waits, or fails, when a
worker of the previous version picks it up.

### License

The API and the worker start whatever the license, and read it on every request. See
[Licensing](/deploy/licensing) for what a valid license is needed for, and for
`EE_LICENSE_FILE`, which lets a renewed license take effect without a restart.

### Roles

The access rules of the four roles are part of the product. The rule rows that earlier
versions stored for each account stay in the database and are no longer read: a rule
added there by hand has no effect. The role of each member is kept.

Setting a role that does not exist is refused with `invalid_argument`.

In the web app, the role controls are shown when `RBAC_ENABLED` is `true`. See
[Environment Variables](/deploy/environment-variables).

### Job hooks and account hooks

Clients that call the hook procedures of the API should expect these answers:

- `not_found` for a hook, a job or an account the caller cannot see
- `already_exists` for a name already taken
- `invalid_argument` for a missing or invalid configuration, and for a webhook URL that
  is not an `http` or `https` URL with a host

Turning a hook on asks for the same permissions and the same license as modifying it.

The secret of a webhook is shown only to callers who may edit the account; others read a
mask. A request that sends the mask back as the secret is refused: send the secret itself
when updating a webhook.

Webhook deliveries carry three more headers, are not repeated after an answer that cannot
succeed, and do not follow redirects. See [Account Hooks](/guides/account-hooks).

### Presidio

The API calls the Presidio analyzer only. `PRESIDIO_ANONYMIZER_URL` is no longer read. See
[Environment Variables](/deploy/environment-variables).

### The analyzer image

If you run the analyzer image of `docker/presidio-fr/`, rebuild it:
`docker compose -f compose.dev.yml up -d --build presidio-analyzer`. Building needs the
network; running does not. The service of `compose.dev.yml` now carries
`pull_policy: build`, so `up` builds the image every time. With a compose file of your own
that does not, or with `docker run`, an image built before this version is started as it
is: it starts, answers on `/health`, and answers every French call with a 500 error,
"No matching recognizers were found". Rebuilding the image is the fix.

The image recognizes French persons with a language model, so it is larger and slower per
French text. Measured once on one host, against the image before the change: 3.9 GB
instead of 1.69 GB, 1.31 GiB of memory after start-up instead of 1.12 GiB, and 8 to 10 s
before `/health` answers instead of 6.5 s without a CPU quota (12 to 24 s on one CPU, 36 s
on half a CPU). Each analyzer process loads the model: size `WORKERS` and
`OMP_NUM_THREADS` of the image as its `README.md` says.

The time a French text takes now grows with its length. Observed per 1,000 characters:
0.3 s for prose and 0.7 to 0.8 s for text with little whitespace, such as compact JSON,
without a CPU quota; 0.9 to 1.2 s for prose and 1.8 s for compact JSON on one CPU; 2.2 s
for prose on half a CPU. A 2,000-character prose text took 0.56 s instead of 0.07 s; that
ratio holds for short values only, and a long text or one with little whitespace is 20 to
50 times slower than with the previous engine. Husonym waits 60 seconds for the analyzer:
a text whose analysis takes longer fails the value, where the previous engine answered.
Check the longest French values of the columns you map to `Transform PII Text` against
these rates and the CPUs you give the analyzer. The image's worker timeout is now a
setting, `WORKER_TIMEOUT`, 120 seconds by default.

The configuration is now inside the image. Files that an older compose file mounted over
it are no longer needed, and would override it: remove those mounts.

French verdicts and rewritten passages change. On the invented business text of the
image's measure, 1 value of 300 in columns that name no person was designated as a person,
against 32 before; fewer product and company names are taken for persons. The gain has a
price: on the same text the previous engine found 32 of the 32 names, among 106 passages
of which 32 were on a name, and the new one finds 31 of 32. A name can occasionally be
left as it is where it used to be rewritten; the measured case is a family name in
capitals placed first, before the given name. Review the verdicts of the PII content scan
again, and the output of `Transform PII Text` on French text. See
[Transform PII Text](/transformers/system#transform-pii-text).

### Microsoft SQL Server destinations

Initializing the schema needs a source database at compatibility level 130 or more, and
a source login that holds `VIEW DEFINITION` on it. See
[Schema initialization](/schema-init/overview).

### Columns mapped by AutoMap

With **AutoMap & Review**, a run decides the mapping of each column added to the source
since the job was configured. More column names are now recognized as personal data, in
eight languages, and every recognized column of a known type is rewritten. On PostgreSQL
and MySQL, a column held by a CHECK constraint is copied as is, and the run log names it.

After the first run that follows the upgrade, review the mappings the run has written into
the job. See [New Column Addition Strategies](/guides/new-column-addition-strategies).

### Virtual foreign keys

Before upgrading, review the virtual foreign keys of your jobs. A job whose virtual
foreign key references a table or a column that the source does not hold fails at the
start of the run, with an error that names the table of the key, the table it references
and, for a column, the column. The [pre-flight check](/guides/preflight) stops with the
same error instead of listing it among its findings.

The names are compared exactly as the catalog of the source writes them. A key that names
its referenced table or column in another case than the catalog ran before on a server
that compares names without regard to case; it now stops the run. Spell the names as the
catalog does. See [Virtual Foreign Keys](/table-constraints/virtual-foreign-keys).

### Names and text copied from the source

Tables, columns and other objects whose names hold a quote character, an apostrophe, a
backslash, a space or upper-case letters, and column defaults and enum labels that hold
an apostrophe or a backslash, are copied as they are by schema initialization on
PostgreSQL, MySQL, MariaDB and SQL Server, and by a sync run by the Athanor engine from
PostgreSQL, MySQL or MariaDB.

On MySQL, a check constraint whose expression holds a backslash, a line break or a
character outside ASCII is recreated as on the source, and so is a column default that is
a plain string holding an apostrophe or a backslash. On PostgreSQL, the labels of an enum
type are created in the order of the source; a type created by an earlier version in
another order is not reordered.

A MySQL source whose sessions run under the `NO_BACKSLASH_ESCAPES` SQL mode is read. On a
PostgreSQL source that has `standard_conforming_strings` off, the default of a mandatory
foreign key column that holds a backslash is recognized as the value that stands for "no
parent", as it is with the setting on.

Known limits:

- A schema or table name that holds a dot is not supported.
- On PostgreSQL, a column default or a check constraint whose text holds a backslash is
  copied as it is only between a source and a destination that have the same
  `standard_conforming_strings` setting. From a source that has it on to a destination that
  has it off, the destination refuses the statement or reads the text differently; from a
  source that has it off to a destination that has it on, each backslash is copied twice.
- On MySQL and MariaDB, a column comment that holds a backslash is not copied.
- On MySQL, a function whose parameter names need quoting is not created.
- On SQL Server, a column whose collation name is not made of letters, digits and
  underscores takes the default collation of the destination database, and the run
  reports it.

### Sampled rows

The rows read to recognize personal data (the PII content scan, the column preview and
the PII detection job) are drawn from the whole table where the database allows it, so
two scans of the same table can read different rows. See
[Data sampling](/guides/pii-detection-job#data-sampling).

### PII detection jobs

See [Upgrading](/guides/pii-detection-job#upgrading) in the guide of the PII detection job.
In the web app, the job is offered when `PII_DETECTION_JOB_ENABLED` is `true`.
