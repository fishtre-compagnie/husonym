---
title: Licensing
description: How to install your Husonym license, and what happens as it approaches and passes its expiry date
id: licensing
hide_title: false
slug: /deploy/licensing
---

## Installing your license

Your license is a single base64 value. Set it as the `EE_LICENSE` environment variable on
the API:

```yaml
environment:
  EE_LICENSE: <the value provided to you>
```

Alternatively, put the license in a file and set `EE_LICENSE_FILE` to its path:

```yaml
environment:
  EE_LICENSE_FILE: /etc/husonym/license
```

When both variables are set, the file wins over `EE_LICENSE`.

`EE_LICENSE` is read once, when the service starts. The file named by `EE_LICENSE_FILE`
is read again at most once a minute, so a renewed license is picked up **without a
restart**: replace the content of the file and the new key takes effect within about a
minute. A key that cannot be verified, or a file that is empty or unreadable, is ignored
and reported in the logs. The key already in place stays in force.

The service always starts, whatever the license: absent, unreadable or expired. In the
first two cases it logs the reason and runs without a license.

The license is checked on every request and follows the clock. An expiry never needs a
restart. A renewal needs none only with `EE_LICENSE_FILE`.

Verification happens entirely offline. Husonym never contacts us to check your license, so
it works in an air-gapped environment, and we collect nothing about how you use it.

:::note
The worker needs no license variable. It obtains the key from the API, with its API key,
when it starts and then once a minute, and verifies it itself. A renewed license therefore
reaches the worker without a restart. An `EE_LICENSE` or `EE_LICENSE_FILE` variable left in
place on the worker is ignored and does no harm.

While the worker has no license, for instance when the API does not answer at its start,
data-sync job runs still execute, but their job hooks and the account-hook notifications
are skipped, and initializing the schema of a Microsoft SQL Server destination fails. A
PII detection job run fails. The worker obtains the license as soon as the API answers.
:::

## What the license covers

The rule is the same for every request: **creating, modifying and executing** require a
valid license; **reading, stopping and deleting** never do. In practice, a valid license
is needed to create, configure and run jobs — the core of the product — as well as to
create or modify job and account hooks, to create or modify Amazon S3 and Google Cloud
Storage connections, to initialize the schema of a Microsoft SQL Server destination, and
to use the bulk anonymization call and the PII text transformer — whether it is mapped
directly, stored in a user-defined transformer or called from a script. Your license may also
restrict which connection types you can create (see [Usage limits](#usage-limits)).

Husonym itself does not depend on the license to start. Authentication, run logs and
metrics are available whether or not a license is installed, and the access rules (roles)
apply with or without one.

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

These are refused with the message `account does not have an active license`. Also
refused, each with its own message: initializing the schema of a Microsoft SQL Server
destination, the bulk anonymization call, and the PII text transformer.

**Keeps working:**

- viewing every job, run, run log, connection, hook and mapping in your history
- pausing a schedule, and turning a hook off
- cancelling or terminating a run that is already going
- deleting jobs, hooks and connections

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

## Renewing, or asking a question

Write to [contact@husonym.com](mailto:contact@husonym.com). Renewing means replacing the
license value: with `EE_LICENSE_FILE`, replace the content of the file and the API picks it
up on its own; with `EE_LICENSE`, change the value and restart the API. The worker follows
the API on its own. Nothing else changes.

If you have lost your license value, ask us rather than assuming a new one is needed: we
keep a record of what was issued and can re-send it.
