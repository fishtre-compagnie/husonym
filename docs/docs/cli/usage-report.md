---
title: usage-report
description: Learn how to write the usage report of an instance for a period to a file with the husonym usage-report CLI command.
id: usage-report
hide_title: true
slug: /cli/usage-report
---

# husonym usage-report

## Overview

Learn how to write the usage report of an instance for a period to a file with the husonym usage-report CLI command.

The `husonym usage-report` command writes the usage report of an instance for a period of months to a file.

The file has two lines:

1. The report, exactly as the API returned it.
2. A JSON object, `{"seal":"…","key_fingerprint":"…"}`, holding the seal of the report and the fingerprint of the key that made it.

The file is created with permissions `0600` and is never overwritten unless `--force` is given. The report is not changed between the API and the file, so it can be read before the file is handed over: use `--print` to see an indented copy of it on standard output.

## Usage

```bash
husonym usage-report --from 2026-01 --to 2026-12 --output report.json
```

To read the report without writing a file:

```bash
husonym usage-report --from 2026-01 --to 2026-12 --print
```

## Options

The following options can be passed using the `husonym usage-report` command:

- `--from` - First month of the period, as `YYYY-MM`. Required.
- `--to` - Last month of the period, as `YYYY-MM`. Required, and not before `--from`.
- `--output`, `-o` - File to write the report to.
- `--print` - Print the report, indented, to standard output. It can be used with or without `--output`.
- `--force` - Overwrite the output file if it exists.
- `--account-id` - Account to report on. Defaults to the account id in the CLI context.
- `--api-key` - Husonym API Key. Takes precedence over `$HUSONYM_API_KEY`

At least one of `--output` and `--print` is required.

## Environment Variables

| Variable        | Description                                                                                              | Is Required | Default Value         |
| --------------- | -------------------------------------------------------------------------------------------------------- | ----------- | --------------------- |
| HUSONYM_API_URL | The base url of the Husonym API. This can be overridden to connect to different Husonym API environments | false       | http://localhost:8080 |
| HUSONYM_API_KEY | The api key for Husonym API.                                                                             | false       |                       |
