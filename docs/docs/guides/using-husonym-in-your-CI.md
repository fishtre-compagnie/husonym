---
title: Using Husonym in CI
description: Learn how to use Husonym in your CI pipelines in order to hydrate your CI databases with anonymized production data
id: using-husonym-in-ci
hide_title: false
slug: /guides/using-husonym-in-ci
---

## Introduction

Continuous Integration is a primary usecase for utilizing Husonym. It's often the case that integration or unit tests run in CI that need good data.
It's easy enough to spin up a Postgres or other kind of database using Github Actions, but the problem becomes hydrating that database with solid data that can be used for testing purposes.

For this reason, we built the [husonym sync](../cli/sync.md) command to enable synchronizing a connection configured in Husonym to a locally hosted database, or any other database that may not otherwise be available over the internet easily.

Getting the CLI installed into a Github Action is task in itself, however. This is why we are offering a first class way to install the Husonym CLI.

## Husonym CLI Github Action

We've built the [Setup Husonym CLI Action](https://github.com/nucleuscloud/setup-husonym-cli-action) Github Action that can be easily dropped into any job to immediately get setup with the Husonym CLI.
The README for that action gives detailed instructions on how to get that set up. Afterwards, any `husonym` command can be run in subsequent jobs.

## Setup a Github Action to sync remote data to a CI Postgres Database

This is a full example of a Github Action that pulls down data from a remotely configured Husonym Connection and hydrates the local Postgres database.

If you want to try this with a sample dataset, there is a good one on [sqltutorial.org](https://www.sqltutorial.org/sql-sample-database/)

```yaml
name: Setup Husonym CLI and PostgreSQL Database
​
on:
  push:
    branches: [main]
  pull_request:
    branches: [main]
​
jobs:
  setup:
    runs-on: ubuntu-latest
​
    services:
      postgres:
        image: postgres
        env:
          POSTGRES_DB: husonym
          POSTGRES_USER: postgres
          POSTGRES_PASSWORD: postgres
        ports:
          - 5432:5432
        # Set health checks to wait until postgres has started
        options: >-
          --health-cmd pg_isready
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5
​
    steps:
      - name: Checkout
        uses: actions/checkout@v4
​
      - name: PostgreSQL Schema Setup
        run: |
          PGPASSWORD=postgres psql -h localhost -U postgres -d husonym -f sql/create.sql

      - name: Select from Employees table to see that it's empty
        run: |
          PGPASSWORD=postgres psql -h localhost -U postgres -d husonym -c 'SELECT * from husonym.employees;'
​
      - name: Set up Husonym CLI
        uses: nucleuscloud/setup-husonym-cli-action@v1
​
      - name: Husonym sync command
        run: husonym sync --api-key ${{ secrets.HUSONYM_API_KEY }} --connection-id <connection-uuid> --destination-driver postgres --destination-connection-url "postgresql://postgres:postgres@localhost:5432/husonym?sslmode=disable"
        env:
          HUSONYM_API_URL: <husonym-api-url>
​
      - name: Select from PostgreSQL
        run: |
          PGPASSWORD=postgres psql -h localhost -U postgres -d husonym -c 'SELECT * from husonym.employees;'
```
