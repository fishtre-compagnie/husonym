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

| Tool                  | Description                                                                                                                  |
| --------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| `list_connections`    | Lists the connections of the account: id, name and category.                                                                 |
| `describe_connection` | Describes one connection: host, port, database, user, tunnel, TLS and options, with every secret masked.                     |
| `introspect_schema`   | Lists the tables of a SQL connection, or gives the columns, types and keys of up to 20 of them, foreign keys in both directions. |
| `suggest_mappings`    | Says which columns hold personal data and which transformer fits each, with how sure the detection is and why. Key columns are flagged. |
| `preview_column`      | Shows what a transformer makes of real values of a column, and whether it collapses distinct values together. Asks the person first. |

Every tool reads; none writes.

Connection credentials are write-only through this server: none of its tools reads them back.

Only `preview_column` returns values read from rows, and those values reach the model — and whoever serves it, unless it runs on your machine. So the server never decides alone: before the first read on a connection, it asks you through your MCP client, and your answer holds for that connection until the session ends. Decline, and nothing is read. A client that cannot ask you gets a refusal, never a read. `suggest_mappings` can also have the API scan a sample of a table (`scan_content`), but it reports what it found as counts and labels, never the values themselves.

## Environment Variables

| Variable        | Description                                                                                              | Is Required                                   | Default Value         |
| --------------- | -------------------------------------------------------------------------------------------------------- | --------------------------------------------- | --------------------- |
| HUSONYM_API_URL | The base url of the Husonym API. This can be overridden to connect to different Husonym API environments | false                                         | http://localhost:8080 |
| HUSONYM_API_KEY | The api key for Husonym API. The server does not fall back on the session of `husonym login`.            | true, unless the API has authentication off |                       |
