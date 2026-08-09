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

Restart both services afterwards; the license is read at startup.

Verification happens entirely offline. Husonym never contacts us to check your license, so
it works in an air-gapped environment, and we collect nothing about how you use it.

:::note
The license must be set on the worker as well as the API. With it missing from the worker,
jobs are accepted but never execute.
:::

## What the license covers

An active license is required to **create, configure and run jobs** — the core of the
product — as well as role-based access control, Microsoft SQL Server connections, job and
account hooks, and run logs.

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
never touches your data.

**Stops:**

- creating new jobs, and changing the configuration of existing ones
- starting new job runs, manually or on a schedule
- resuming a paused schedule

**Keeps working:**

- viewing every job, connection, mapping and run in your history
- pausing a schedule
- cancelling or terminating a run that is already going
- deleting jobs and connections

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
`EE_LICENSE` value and restarting the API and the worker — nothing else changes.

If you have lost your license value, ask us rather than assuming a new one is needed: we
keep a record of what was issued and can re-send it.
