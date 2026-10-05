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
and, for a column, the column. The pre-flight check of the job fails the same way.

The names are compared exactly as the catalog of the source writes them: on a server that
compares names without regard to case, spell them as the catalog does. See
[Virtual Foreign Keys](/table-constraints/virtual-foreign-keys).

### Names and text copied from the source

Tables, columns and other objects whose names hold a quote character, an apostrophe, a
backslash, a space or upper-case letters, and column defaults and enum labels that hold
an apostrophe or a backslash, are copied as they are by schema initialization and by a
sync.

Two limits remain:

- On MySQL and MariaDB, a column comment that holds a backslash is not copied.
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
