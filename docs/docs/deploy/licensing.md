---
title: Licensing
description: How to install your Husonym license, and what happens as it approaches and passes its expiry date
id: licensing
hide_title: false
slug: /deploy/licensing
---

## Installing your license

Your license is a single base64 value. Set it as the `EE_LICENSE` environment variable on
**both** the API and the worker:

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

The license is checked on every request and follows the clock. An expiry, like a renewal,
takes effect without a restart.

Verification happens entirely offline. Husonym never contacts us to check your license, so
it works in an air-gapped environment, and we collect nothing about how you use it.

:::note
The license must be set on the worker as well as the API. With it missing from the worker,
jobs are accepted but never execute.
:::

## What the license covers

The rule is the same for every request: **creating, modifying and executing** require a
valid license; **reading, stopping and deleting** never do. In practice, a valid license
is needed to create, configure and run jobs — the core of the product — as well as to
create or modify job and account hooks, to use Microsoft SQL Server connections, and to
use PII detection and the anonymization calls that rely on it.

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

**Refused** with the message `account does not have an active license`:

- creating new jobs, and changing the configuration of existing ones
- starting new job runs, manually or on a schedule
- resuming a paused schedule
- creating or modifying a hook, and turning a hook back on
- PII detection, and the anonymization calls that need it

**Keeps working:**

- viewing every job, run, run log, connection, hook and mapping in your history
- pausing a schedule, and turning a hook off
- cancelling or terminating a run that is already going
- deleting jobs, hooks and connections

Runs already in progress when the license expires are allowed to finish rather than being
interrupted mid-sync.

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
license value: with `EE_LICENSE_FILE`, replace the content of the file and the API and the
worker pick it up on their own; with `EE_LICENSE`, change the value and restart the API
and the worker. Nothing else changes.

If you have lost your license value, ask us rather than assuming a new one is needed: we
keep a record of what was issued and can re-send it.
