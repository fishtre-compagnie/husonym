---
title: Authentication
description: Learn how to configure and maintain authentication within Husonym open source
id: auth
hide_title: false
slug: /deploy/authentication
---

## Introduction

By default, Husonym launches without requiring any form of authentication. This is to make getting started with Husonym easier, and it's not advised to run Husonym in production without any form of authentication.

There are a few different authentication systems and play here, and this doc will detail what each one is, and why it exists.
There will also be a section for how to properly set up these systems properly.

## User Authentication

> **NB:** This requires a valid Husonym Enterprise license for OSS deployments. If you would like to try this out, please contact us.

This authentication is the primary form of authentication in the system. It is used to identify authenticating users in the system and what privileges they have.
Today, this is quite simple and merely verifies their access token is valid and that they are operating against a Husonym account that a user resides in.

In the future, it is planned to add more fine-grained authentication in the form of scopes. This will enable more granular authorization against specific actions in the system against targeted resources.

This form of authentication is configured with any Open Identity Connect (OIDC) protocol compliant provider.
Internally, we utilize Auth0, and to date this is the only provider that has been fully tested for use with Husonym.

Environment variables must be provided for the App ad API to properly configure User Authentication.
See the [environment variables](/deploy/environment-variables.md) page for the whole list of required auth env vars.
The table shows that most of them are not required, however that is only true if `AUTH_ENABLED` is set to false. They must be provided if `AUTH_ENABLED=true`.

The descriptions of each environment variable detail exactly what the `AUTH_*` is and why it is needed.

If there is any trouble configuring authentication with Husonym, reach out to us for help at support@husonym.com.

## How to Authenticate against Husonym API

The API expects the standard `Authorization` header to come in via the HTTP request. The format should be `Bearer <token>`. This is true for both API Keys as well as user JWTs.

## APP Auth Configuration

The app requires a bit more configuration since there is also session management that occurs with the help of `next-auth`.
Today, the App has only been tested with Auth0, however, theoretically any auth provider credentials could be entered for the environment variable values and it should work.
We are working towards making this more generic to allow more providers like Keycloak, Google, etc.

## API Key Authentication

This authentication is the primary form of authentication for system-level access. It is used to identify and authenticate machine users.
Today, the primary function for this is for use with the Worker process, as well as CLI-access in an automated environment like Github Actions.
This form of authentication also makes writing scripts with the Husonym SDKs much easier and it isn't as easy to provide a user access token.

API Keys have their own Husonym User Identifier assigned to them and are scoped to the Husonym Account.
They have a maximum expiration of 1 year before they expire and require rotation.
It's advised to rotate the keys prior to that, or simply create a new one to allow overlap as once the key has been rotated, the old one will no longer work.

### Configuration

It's important to note here that API Key is not enabled unless the `AUTH_ENABLED` environment variable is set to `true` in the API.
To enable auth, User authentication must be properly configured as well. Both systems are turned on or off.

An API Key can be created either through the SDK or via the web app.

To do so via the web app:

1. navigate to the relevant instance of Husonym
2. go to the `Settings` page for the account that it is desired to create an API key for.
3. Click the API Keys section
4. Click the `+ New API Key` button.
5. Write down a name and select when it should expire
6. Check the permissions the key needs
7. Submit

If successful, you should now be on the API Key Details page and the API Key should be seen in plaintext on the page.

It's important to save this somewhere as it is no longer retrievable again. If lost, a new key must be regenerated.
These keys are not stored in plaintext in the database and are one-way hashed so the original contents are no longer retrievable.

### Permissions

A key can do only what its permissions name, whatever the rest of the configuration allows. A permission is one action on one kind of entity:

| Entity      | Permissions                                                                                                                    |
| ----------- | ------------------------------------------------------------------------------------------------------------------------------ |
| Jobs        | `job:view`, `job:create`, `job:edit`, `job:execute` (runs a job), `job:delete`                                                 |
| Connections | `connection:view`, `connection:view_sensitive` (secrets in clear), `connection:create`, `connection:edit`, `connection:delete` |
| Account     | `account:view`, `account:edit`, `account:create`, `account:delete`                                                             |

- A key needs at least one permission, and can hold none that its creator does not hold themselves.
- Using a connection takes its secrets: reading its schema, scanning or previewing its data, `husonym sync` all need `connection:view_sensitive`. Without it, a key reads connections with their passwords and keys masked, and cannot connect to them.
- Running a job takes `job:execute`, and so does anything that makes it run: creating it with a first run or an active schedule, setting its schedule, resuming it.
- `account:edit` lets a key manage members and their roles, admin included: grant it as you would admin.
- A call the key's permissions do not cover is refused, and the refusal names the permission that is missing.
- Keys created before permissions existed hold all of them. Narrow them by creating new keys with only what they need.
- The worker needs its worker key, or an account key holding every permission.

## Temporal mTLS Authentication

Husonym API and Husonym Worker both require mTLS authentication when interfacing with Temporal (if this is enabled in Temporal).
Like Husonym, by default, the local versions of Temporal don't require authentication by default (although this may be changing.)
If using Temporal Cloud, mTLS is required by default and must be configured to properly communicate with the Temporal servers.

### mTLS Certificate Configuration

Temporal has a guide for creating mTLS certs [here](https://docs.temporal.io/cloud/certificates#use-tcld-to-generate-certificates).

Once these have been created, they must be provided as environment variables to both the API and Worker processes.
Reference the [environment variables](/deploy/environment-variables.md) page for the `TEMPORAL_*` environment variables.

## Auth Server Admin Access

The name, email address and picture shown on the members page come from the standard OIDC
claims (`name`, `email`, `email_verified`, `picture`), read when a user signs in — from the
token when it carries them, otherwise from the provider's `userinfo` endpoint, which is
found through its discovery document. Any OIDC-compliant provider works, and no extra
configuration is needed.

The `AUTH_API_*` variables below configure a **fallback**, used only for a user who has not
signed in since these values started being stored, or whose provider sends no profile at
all. They are optional: omitting them means such a member is listed without a name.

That fallback is an administration API of the provider's own — not part of OIDC — so it
exists only for the two products Husonym implements it for: Keycloak and Auth0.

This is determined by the `AUTH_API_PROVIDER` environment variable that recognizes `auth0` and `keycloak` as their values. If omitted, `auth0` is the default for backwards compatibility.

The following environment variables are as follows:

- `AUTH_API_BASEURL` - This is the base url for the Admin API. Auth0 calls this the Management API, while Keycloak the Admin API.
  - For auth0, this is almost always your raw tenant url as custom domains do not work with Auth0 management API access. Example: `https://your-tenant.us.auth0.com`
  - For keycloak, this url will look something like this: `https://auth.svcs.stage.husonym.com/admin/realms/husonym-stage`. The pattern is: `<baseurl>/admin/realms/<realm>`
- `AUTH_API_CLIENT_ID` - The service account's client id
- `AUTH_API_CLIENT_SECRET` - The client id secret

Scopes:

Today, this client only requires minimal access to the API to read users.
For Auth0, the service account should have the `read:users` scope under the `Auth0 Management API` audience.
For Keycloak, the `view-users` scope should be added to the service account roles, which can be found under the `realm-management` client scopes.

## What Husonym requires of a token

Two things, beyond a valid signature, an expected issuer and an expected audience.

**A token must carry an issuer.** A user is identified by the pair (issuer, subject), not
by the subject alone: whoever operates an identity provider chooses the subjects it
issues, so a subject only means something under the provider that minted it. Tokens
predating this requirement are adopted at their owner's next sign-in, and only by the
issuer the deployment is configured with.

**Accepting an invitation requires a verified address.** An invitation is matched on an
email address, and an address is only worth matching on if the provider asserts it
verified it — `email_verified`, in the token or at the `userinfo` endpoint. A provider
that never asserts it will have its users refused at that step; the rest of the product is
unaffected. The invitation also records the issuer it may be accepted from, so a token
from another provider carrying the same address does not open the account.

A token that states it was issued to an application rather than to a person (`idtyp`) is
refused before it can create a user. A provider that does not state it is unaffected.

## Starting Husonym in Auth Mode

> **NB:** This requires a valid Husonym Enterprise license to be present in the API container. If you would like to try this out, please contact us.

Starting Husonym in Auth Mode is done in a similar way as starting Husonym in non-auth mode: using a compose file. A compose file is also provided that stands up [Keycloak](https://keycloak.org), an open source auth solution.

To stand up Husonym with auth, simply run the following command from the repo root:

```sh
make compose/auth/up
```

To stop, run:

```sh
make compose/auth/down
```

Husonym will now be available on [http://localhost:3000](http://localhost:3000) with authentication pre-configured!
Click the login with Keycloak button, register an account (locally) and you'll be logged in! -->
