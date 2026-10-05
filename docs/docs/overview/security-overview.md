---
title: Security Overview
description: Learn about the security principles of the Husonym Platform
id: cloud-security-overview
hide_title: false
slug: /cloud-security-overview
---

This section documents a few things that we feel are worth mentioning from a security perspective.

## Code

All of the Husonym code can be found on our [Github](https://github.com/fishtre-compagnie/husonym).
If you find a security vulnerability, please refer to our [Security.md](https://github.com/fishtre-compagnie/husonym/blob/main/SECURITY.md) for what to do.
If all else fails, please email `security@husonym.com` directly.

Husonym runs on your own infrastructure: the data it reads and writes, and the credentials of your connections, stay there.
The license is verified offline. See [Licensing](/deploy/licensing).

## Connecting a Production Database to Husonym

We do not recommend connecting a production database directly to Husonym.

This is recommended purely for security purposes, but also due to an increased load that Husonym may put on your database when invoking a sync.
For that reason, we suggest restoring a snapshot of production periodically to another database that is then used by Husonym.
