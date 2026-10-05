---
title: Docker Compose
description: Learn how to deploy Husonym using Docker compose for a better local developer experience
id: docker-compose
hide_title: false
slug: /deploy/docker-compose
---

## Trying Husonym with Compose

A `compose.yml` file is provided at the root of the repository. This uses our pre-built docker images so no building is required.

This file includes two other compose files that are found in the main repository.

- Temporal Compose
- Test Databases

We split out the Temporal compose file to make it easier to include in other places, as well as to keep the main compose clean and to have a separate of concerns.

**This main compose.yml file is made to easily try Husonym and should not be used as-is for production deployments.**

### Choosing the version

The three images (`app`, `api` and `worker`) always run the same version, and you choose it explicitly with the `HUSONYM_VERSION` variable. `compose.yml` refuses to start without it.

Set it to a released version, written without the leading `v`. For example, for the release `v0.2.1`:

```console
HUSONYM_VERSION=0.2.1
```

Put this line in a `.env` file next to `compose.yml` (Compose reads it automatically), or export the variable in your environment.

The list of released versions is on the [releases page](https://github.com/fishtre-compagnie/husonym/releases).

### Starting

To run this you can run one of the two following commands:

```console
make compose/up
docker compose up -d
```

### Moving to a newer version

Migrations of the Husonym database run when the API starts, and they are not reversed when you go back to an older version.
**Back up the Husonym database before every upgrade**: the `db` service (PostgreSQL, database `husonym`). Temporal has its own database, which this step does not cover.

Then change the value of `HUSONYM_VERSION` and run:

```console
docker compose pull
docker compose up -d
```

## Deploying Husonym with Compose

If you wish to deploy Husonym to production with `compose.yml`, we don't currently offer a single `compose.yml` file to do this (yet.).
However, you can easily combine main `compose.yml` and the temporal `compose.yml` files to achieve this.

The main `compose.yml` includes an `api-seed` and `temporal-seed` that may not be necessary and require extra files, so those can be omitted for minimal dependencies.

Once all of the containers come online, the app is now routable via [http://localhost:3000](http://localhost:3000).

## Deploy with Docker Compose and Authentication

Husonym provides an auth friendly compose file that will stand up Husonym in auth-mode with Keycloak.

```console
make compose/auth/up
docker compose -f compose.yml -f compose.auth.yml up -d
```

Like the main `compose.yml`, these commands need `HUSONYM_VERSION` to be set, see [Choosing the version](#choosing-the-version).

Keycloak comes default with two clients that allow the app and cli to login successfully.

On first boot up, Keycloak will assert itself with the provided realm Husonym realm.

When navigating to Husonym for the first time, you'll land on the Keycloak sign-in page. It is easy to create an account simply by going through the register flow.
This will persist restarts due to the postgres volume mapping. If you wish to start over, simply delete the husonym docker volume to reset your database to a fresh state.
