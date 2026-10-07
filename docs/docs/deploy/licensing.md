---
title: Licensing
description: How to install your Husonym license, what it includes, and what happens as it approaches and passes its expiry date
id: licensing
hide_title: false
slug: /deploy/licensing
---

## Installing your license

Your license is a single base64 value, the key. One key is in force for the whole
instance, every account included. There are three ways to install it.

**From the License page.** In the settings of any account, open **License**, paste the key
in the **License key** box and choose **Install key**. The page takes effect at once, with
no restart. Installing a key asks to be an administrator of the account you are signed in
to.

**With the environment variable or the file, on the API.** Set the key as the `EE_LICENSE`
environment variable on the API:

```yaml
environment:
  EE_LICENSE: <the value provided to you>
```

Alternatively, put the key in a file and set `EE_LICENSE_FILE` to its path:

```yaml
environment:
  EE_LICENSE_FILE: /etc/husonym/license
```

These variables do not hold the key in force: the API reads them and offers what they hold
to its database, which keeps the key. `EE_LICENSE` is read once, when the API starts; if the
database does not answer at that moment, the API offers the key again every minute until it
does. The
file named by `EE_LICENSE_FILE` is read when the API starts and again at most once a minute,
so a renewed key is picked up **without a restart**: replace the content of the file and the
new key takes effect within about a minute. When both variables are set, both are offered
and the more recent key is kept.

**Through the API.** Call `SetSystemLicense` of the user account service, with the id of an
account you administer and the key. The answer describes the license now in force; it never
contains the key.

When authentication is not enabled (`AUTH_ENABLED` is `false`), anyone who can reach the API
can install a license key, and read the key in force as it was signed. Authentication should
be enabled in any production environment.

A key replaces the one in force only when it was issued **after** it. An older key, or one
that was issued at the same moment, is refused with a message that gives both dates, and the
key in force is not touched. A key that is not valid, or that was not signed by us, is refused
too. This is also what happens at start: a variable that still holds an older key than the
one the instance already holds is ignored, and the log says so. A key that is newer is taken
even when it has already expired, and the instance then behaves as the stages below describe.

A key that cannot be verified, or a file that is empty or unreadable, is reported in the
logs and the key already in place stays in force. The API always starts, whatever the
license: absent, unreadable or expired. In the first two cases it logs the reason and runs
without a license. When the instance has no license and the key given in `EE_LICENSE` or
in the file was refused as invalid, the License page shows the reason.

The license is checked on every request and follows the clock. An expiry never needs a
restart, and neither does a renewal.

Verification happens entirely offline. Husonym never contacts us to check your license, so
it works in an air-gapped environment, and we collect nothing about how you use it.

:::note
The worker needs no license setting. It obtains the key from the API, with its API key,
when it starts and then once a minute, and verifies it itself. A key installed on the API
therefore reaches the worker without a restart. An `EE_LICENSE` or `EE_LICENSE_FILE`
variable left in place on the worker is ignored and does no harm.

A worker that starts takes no job run until the API has answered it once about the license,
"this instance has no key" being an answer. Until then it asks again every five seconds and
its log says that it is waiting. A job never runs without its hooks: if a run reaches its
job hooks on a worker that holds no license in force, the run fails with
`the worker has not received the instance's license yet: job hooks were not run`.
:::

## The License page

The **License** page of the settings shows the license of the instance and what your
account uses of it.

- **Status**: the stage of the license (see below), the licensee, the plan label, the
  expiry date and, in the grace period, the date the grace period ends, and how the key was
  installed and when.
- **Features**: each feature, whether the license includes it, and which ones your account
  uses. A feature that your account uses, that the license does not include and that keeps
  jobs from starting (job hooks, PII text, PII detection, custom transformers, subsetting)
  is shown first, in red. The other features in use that the license does not include stop
  nothing: what exists keeps working, and changes are refused. The use of the MCP server, of
  mapping review and of run logs is not tracked.
- **Limits**: the sources counted against the number the license allows, the sources of
  this account, and the other limits the license carries. Sources are counted for the whole
  instance; the limits on jobs and on connections apply to each account.
- **License key**: where you install a new key. The key in force is never shown. Pasting
  the key that is already in force changes nothing, and the page says so.

## What the license covers

The rule is the same for every request: **creating, modifying and executing** require a
valid license; **reading, stopping and deleting** never do. In practice, a valid license
is needed to create, configure and run jobs — the core of the product — as well as to
create or modify job and account hooks, to create or modify Amazon S3 and Google Cloud
Storage connections, and to initialize the schema of a Microsoft SQL Server destination.
Your license may also restrict which connection types you can create (see
[Usage limits](#usage-limits)).

Husonym itself does not depend on the license to start. Authentication and metrics are
available whether or not a license is installed.

### Features

On top of that, a license says which of these features it includes. A license that names no
feature includes all of them, and so do all the licenses issued before features existed.

| Feature               | What it covers                                                                                                          |
| --------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| `job_hooks`           | hooks that run SQL before and after a job                                                                               |
| `account_hooks`       | account hooks, which announce the events of the runs                                                                    |
| `pii_text`            | the PII text transformer, in a job, in the column preview and in the anonymization calls                                |
| `pii_detection`       | the PII detection job and the detection call on a connection                                                            |
| `custom_transformers` | user-defined transformers and JavaScript: in a mapping, in a rule, in the column preview and in the anonymization calls |
| `subsetting`          | a WHERE clause on a table of the source                                                                                 |
| `scheduling`          | giving a job a schedule, and resuming a paused one                                                                      |
| `mapping_review`      | reviewing and applying the mappings that a run proposes for new columns                                                 |
| `api_keys`            | creating and regenerating API keys                                                                                      |
| `mcp`                 | the MCP server of the command-line tool                                                                                 |
| `rbac`                | giving a member a role other than administrator                                                                         |
| `sso`                 | declaring an OIDC identity provider for an account                                                                      |
| `run_logs`            | serving the logs of a run                                                                                               |

When a feature is not included:

- Most of its actions are shown disabled in the web app, with a notice that points to the
  License page. Some are not disabled and are refused when you use them, with the message
  below: picking a JavaScript or user-defined transformer in a mapping, trying a rule, and
  starting a job.
- The API refuses them with a message that names the feature:
  `this license does not include job_hooks`.
- With `account_hooks` not included, the account hooks that exist stop announcing the events
  of the runs. They are kept, and announce again once the license includes the feature.
- A job hook can be turned off, in the web app too, without `job_hooks`: the job then
  starts, and the hook keeps its SQL.
- A job that uses it does not start, whether it is run by hand or by its schedule. A job
  uses a feature when it has an enabled hook, maps the PII text transformer, maps a
  user-defined transformer or JavaScript, has a WHERE clause, or is a PII detection job. The
  refusal names every feature the license lacks:
  `this job uses features the license does not include: job_hooks, subsetting`.
- **Reading, stopping, deleting and turning off keep working.** You can still see your jobs,
  runs and configuration, cancel a run, delete a job or a hook, pause a schedule and turn a
  hook off. The one read a license can close is the logs of a run, and only a license in
  force that does not include `run_logs` closes it (see below).

The features `rbac`, `sso` and `api_keys` only forbid making changes: roles that are already
assigned, an identity provider that is already declared and API keys that already exist
keep working. An account that has already declared its identity provider can always replace
it, for instance when the provider changes its issuer or its client id, whatever the
license; only declaring the first one needs `sso`. Likewise `scheduling` is checked when a schedule is set or resumed: a schedule
that already runs keeps running. The logs of a run are not served when the license in force
does not include `run_logs`, while the run, its status and its events stay readable. Once a
license has expired, or without any license, the logs of your runs are readable like the
rest of your history.

## As your license approaches expiry

Husonym does not stop abruptly. It moves through four stages, and the interface tells you
which one you are in.

| Stage            | When                               | What happens                                           |
| ---------------- | ---------------------------------- | ------------------------------------------------------ |
| **Active**       | more than 30 days remaining        | everything works                                       |
| **Expiring**     | within 30 days of expiry           | everything works; a banner shows the date              |
| **Grace period** | after expiry, for a further period | **everything still works**; a banner asks you to renew |
| **Expired**      | after the grace period             | new job runs stop                                      |

The grace period is normally 14 days, and your license may specify a different length.

## What happens if a license expires

Once the grace period ends, Husonym stops starting work. It does not lock you out and it
never touches your data. The instance keeps starting and serving requests.

**Refused:**

- creating new jobs, and changing the configuration of existing ones
- starting new job runs, manually or on a schedule
- resuming a paused schedule
- creating or modifying a hook, and turning a hook back on
- creating or modifying an Amazon S3 or Google Cloud Storage connection

These are refused with the message `account does not have an active license`, and so are
the bulk anonymization call, the PII text transformer and the content scan of a
connection. Initializing the schema of a Microsoft SQL Server destination is refused with
a message of its own. Features are only included while the license is in force: once it
is not, none of them is, and a refusal says that no license is active rather than naming a
feature.

**Keeps working:**

- viewing every job, run, connection, hook and mapping in your history, and the logs of
  your runs
- pausing a schedule, and turning a hook off
- cancelling or terminating a run that is already going
- deleting jobs, hooks and connections
- signing in through the identity provider your account has declared, and replacing that
  provider

Runs already in progress when the license expires are not interrupted. One exception: a
run that maps the PII text transformer asks the API to rewrite each value, and the API
refuses once the license has expired, so that run fails.

Nothing is deleted, and no configuration is lost. Installing a renewed license restores
everything immediately — no data migration, no re-setup.

## Usage limits

Your license may include limits agreed in your contract, such as a maximum number of jobs
or connections, or the set of connection types included. When you reach one, Husonym
declines the action and names the limit so you know what to ask for:

```
this license allows 20 job(s) and 20 already exist;
contact us to raise the limit
```

Reaching a limit never affects anything already running.

### Sources

A license may also cap the number of **sources**. A source is a database that a
synchronization job reads:

- for PostgreSQL, Microsoft SQL Server and DynamoDB, a connection that is the source of a
  synchronization job;
- for MySQL and MongoDB, a connection together with a database that the mappings of a job
  read, so one connection read through two databases is two sources.

Destinations are not sources, and neither are the jobs that generate data or detect PII.
Two connections to the same database are two sources. The count covers the whole
instance, every account included, and the License page shows it next to the sources of
your account.

Husonym declines a change that brings a source the instance does not have when the total
would then exceed the cap:

```
this license allows 5 source(s) and this change would bring the instance to 6;
contact us to raise the limit
```

A job that is already configured is never stopped for this, and a run is never refused for
it. If the cap is lowered below what the instance counts, everything keeps running and you
can still edit your jobs, as long as an edit adds no source. The License page then warns that
the instance counts more sources than the license allows.

## Renewing, or asking a question

Write to [contact@husonym.com](mailto:contact@husonym.com). Renewing means installing the
new key, which is issued after the one in force: paste it on the License page, or with
`EE_LICENSE_FILE` replace the content of the file and the API picks it up on its own, or
with `EE_LICENSE` change the value and restart the API. The worker follows the API on its
own. Nothing else changes.

If you have lost your license value, ask us rather than assuming a new one is needed.
