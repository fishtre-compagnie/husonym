---
title: whoami
description: Learn how to display the currently logged in user with the husonym whoami CLI command.
id: whoami
hide_title: true
slug: /cli/whoami
---

# husonym whoami

## Overview

Learn how to display the currently logged in user with the husonym whoami CLI command.

The `husonym whoami` command is used to show the currently logged in user.

## Usage

```bash
husonym whoami
```

## Options

The following options can be passed using the `husonym whoami` command:

- `--api-key` - Husonym API Key. Takes precedence over `$HUSONYM_API_KEY`

## Environment Variables

| Variable        | Description                                                                                              | Is Required | Default Value         |
| --------------- | -------------------------------------------------------------------------------------------------------- | ----------- | --------------------- |
| HUSONYM_API_URL | The base url of the Husonym API. This can be overridden to connect to different Husonym API environments | false       | http://localhost:8080 |
| HUSONYM_API_KEY | The api key for Husonym API.                                                                             | false       |                       |
