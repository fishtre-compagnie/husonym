---
title: login
description: Learn how to login to husonym via the husonym login command.
id: login
hide_title: false
slug: /cli/login
---

# husonym login

## Overview

Learn how to login to husonym via the husonym login command.

The `husonym login` command is used to login to husonym through the CLI to generate an access token.
This can then be utilized with other commands to perform authenticated requests to Husonym API.

## Usage

```bash
husonym login
```

Running this command will open up a browser window and direct the user through the login flow that has been configured by the API.

If on success, an access token and (optionally) a refresh token will be saved to the filesystem in `$HUSONYM_CONFIG_DIR`.

## Environment Variables

| Variable            | Description                                                                                              | Is Required | Default Value         |
| ------------------- | -------------------------------------------------------------------------------------------------------- | ----------- | --------------------- |
| HUSONYM_API_URL     | The base url of the Husonym API. This can be overridden to connect to different Husonym API environments | false       | http://localhost:8080 |
| HUSONYM_API_KEY     | The api key for Husonym API.                                                                             | false       |                       |
| LOGIN_HOST          | The http server that is booted up running `husonym login` via an oauth flow                              | false       | 127.0.0.1             |
| LOGIN_REDIRECT_HOST | The redirect host that is sent alongside the oauth flow when running `husonym login`                     | false       | 127.0.0.1             |
| LOGIN_PORT          | The port the http server runs on when running `husonym login`                                            | false       | 4242                  |
