---
title: Serve
description: Learn how to serve Husonym to an agent over the Model Context Protocol with the husonym mcp serve command.
id: serve
hide_title: false
slug: /cli/mcp/serve
---

## Overview

Learn how to serve Husonym to an agent over the Model Context Protocol with the husonym mcp serve command.

The `husonym mcp serve` command runs a [Model Context Protocol](https://modelcontextprotocol.io) server on stdin and stdout. It is not meant to be run by hand: an MCP client, such as an AI assistant, starts it and talks to it.

The server answers for the account of the API key it is given, with that key's rights.

Give the server a key of its own, with only the permissions it needs:

- reading connections: `connection:view` lists and describes them, and `connection:view_sensitive` is needed to use one — check it, introspect its schema, suggest mappings, preview a column, configure a job on it — since that takes its secrets. The server never hands those secrets to the agent: it reads connections with them masked.
- configuring jobs: `job:create` to create one, `job:view` and `job:edit` to change its mappings, `job:view` with `connection:view` and `connection:view_sensitive` to check one before it runs.
- running jobs: `job:execute`. Leave it out, and the agent prepares jobs that only a person can run.

A tool the key does not allow is refused by the API, which names the permission missing. See [API key permissions](/deploy/authentication#permissions).

## Usage

```bash
husonym mcp serve
```

A client is usually configured with the command and its environment, for instance:

```json
{
  "mcpServers": {
    "husonym": {
      "command": "husonym",
      "args": ["mcp", "serve"],
      "env": {
        "HUSONYM_API_URL": "https://husonym.example.com",
        "HUSONYM_API_KEY": "<your api key>"
      }
    }
  }
}
```

## Tools

| Tool                  | Description                                                                                                                              |
| --------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| `list_connections`    | Lists the connections of the account: id, name and category.                                                                             |
| `describe_connection` | Describes one connection: host, port, database, user, tunnel, TLS and options, with every secret masked.                                 |
| `check_connection`    | Says what a connection lacks to play its role in a job — read its tables as a source, write, empty or create them as a destination.      |
| `introspect_schema`   | Lists the tables of a SQL connection, or gives the columns, types and keys of up to 20 of them, foreign keys in both directions.         |
| `suggest_mappings`    | Says which columns hold personal data and which transformer fits each, with how sure the detection is and why. Key columns are flagged.  |
| `preview_column`      | Shows what a transformer makes of real values of a column, and whether it collapses distinct values together. Asks the person first.     |
| `create_job`          | Creates a job from a PostgreSQL or MySQL source to PostgreSQL or MySQL destinations, with a mapping for every column of every table.     |
| `update_job_mappings` | Maps columns of a job anew, or maps a table it did not read yet; the other mappings stay. Asks the person first if the job is scheduled. |
| `preflight_job`       | Says what a run of a job would meet, without running it: what stops it, what may go wrong, and what it does to the destination.          |
| `run_job`             | Runs one job now. Asks the person first, every time.                                                                                     |
| `get_run_status`      | Gives the latest runs of a job and what the most recent one is doing, without failure messages.                                          |
| `get_run_failure`     | Says why a run failed, in the words of the databases and of the engine. Asks the person first.                                           |

Connection credentials are write-only through this server: none of its tools reads them back.

### Jobs

A job created by the agent has no schedule and does not run on creation. Every column of every table it reads takes a mapping, `passthrough` included: nothing is copied in clear without someone deciding it. Before writing a job's mappings, the server has the API check them as it checks them for the job builder, and refuses what the builder would not offer: a transformer that does not fit its column — a generated column, an identity, a foreign key, a column without a default, a type the transformer does not take — a required column or foreign key left out. A transformer that runs code of its own (`transform_javascript`, `generate_javascript`) is refused: code written by an agent would run with the rows in hand.

A column that appears later in a mapped table halts the run, unless the job is created with `new_columns: auto_map` — which maps it as the PII detection suggests, and copies it as it is when the detection suggests nothing or the column is in a key. A column that disappears from the source is carried on past, as in the job builder. A destination may empty its tables before writing (`truncate_before_insert`), never with a cascade, and never when it points at the database the job reads.

Running a job writes into real databases, so the agent never runs one alone. Before each run, the server asks you through your MCP client, showing all the run does, not only what the agent changed: what it reads, how many columns it copies as they are or runs through code written in the job, where it writes, which destinations it empties or creates tables in, and the SQL hooks it runs. Your yes covers that one run of the job exactly as it stood when you were asked — mappings, destinations, options and hooks; the next run, or a job changed since, asks again. Changing the mappings of a job that runs on a schedule is as good as running it, and asks the same way. A client that cannot ask you gets a refusal.

The mappings of a job are not changed while a run of it is going or starting, since the run reads them when it begins — nor while another call of the agent is running or changing the job: the call that comes second is refused, to be made again; and they are written only if the job is still as the agent read it — a change made meanwhile, in the job builder or by a run mapping a new column, is not overwritten.

`run_job` names the run it starts (`run_id`), and refuses while a run of the job is in progress, while another call is running or changing the job, or while the run it just triggered does not show among the job's runs yet: the list sees a new run a moment late. A trigger that fails for another reason than a missing permission may have started a run, and holds the job the same way, for two minutes at most. `get_run_status` follows a job, not a run: the run started is the one of that id in its list. It names the tables whose sync recorded an error (`failing_tables`); `get_run_failure` says why.

### Checks before a run

`check_connection` and `preflight_job` log in to the connections and read what the account may do there, never a row, and write nothing. The first checks one connection in the role it will play, before the job exists; the second checks a job whole, the way its run checks it at its start ([pre-flight check](/guides/preflight)) — its connections, its engine, and what its mappings do to the destination — and takes up to three minutes, since a worker computes the plan of the run; a call made while a check of the same job is going waits for it. Connections are checked on MySQL and PostgreSQL only: `check_connection` refuses other kinds. A run stops on the same blocking findings, so checking first spares a run that would stop anyway; `run_job` does not check by itself.

A finding may come with the statement that grants what the account lacks, and nothing more. The agent is told to show it to you, never to run it. Why a connection could not be reached, or why a check could not end, is not handed to the agent, since the database driver can quote where and how it connects: testing the connection, or the page of the job, in the UI shows it. The names in the findings — tables, columns, connections — come from the databases, and the agent is told to read them as data.

### Values from rows

Two tools return values read from rows: `preview_column`, and `get_run_failure`, since a database quotes the value a write failed on. Those values reach the model — and whoever serves it, unless it runs on your machine. So the server never decides alone: before the first read on a connection, it asks you through your MCP client, and your answer holds for that connection until the session ends, whichever of the two tools reads. For a run, the connection is the source of its job. Decline, and nothing is read. A client that cannot ask you gets a refusal, never a read. `suggest_mappings` can also have the API scan a sample of a table (`scan_content`), but it reports what it found as counts and labels, never the values themselves.

### Time limits

The server waits for each call it makes to the API for a limited time, so that a call the API never answers ends, and tells the agent so, instead of holding the tool and the job it runs or changes: 30 seconds for what the API answers by itself, 2 minutes for what it answers by reading the database of a connection — its schema, a sample scanned for personal data, a preview, the check of mappings — a little over 2 minutes for `check_connection`, and a little over 3 for `preflight_job`. A tool that makes several calls, such as `suggest_mappings` scanning table after table, may take longer in all.

A trigger cut short by its limit may have started a run: `run_job` then holds the job as for any trigger that failed, for two minutes at most.

An MCP client may itself give up on a tool call after a time of its own. To a client that asks for the progress of a call (a `progressToken`), the server tells every 15 seconds that the call is still worked on, which the protocol lets the client count as a reason to wait longer.

## Environment Variables

| Variable        | Description                                                                                              | Is Required                                 | Default Value         |
| --------------- | -------------------------------------------------------------------------------------------------------- | ------------------------------------------- | --------------------- |
| HUSONYM_API_URL | The base url of the Husonym API. This can be overridden to connect to different Husonym API environments | false                                       | http://localhost:8080 |
| HUSONYM_API_KEY | The api key for Husonym API. The server does not fall back on the session of `husonym login`.            | true, unless the API has authentication off |                       |
