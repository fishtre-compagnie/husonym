---
title: Version
description: Learn how to view the Husonym CLI Version with the husonym version command and husonym --version flag.
id: version
hide_title: false
slug: /cli/version
---

# husonym version

## Overview

Learn how to view the Husonym CLI Version with the husonym version command and husonym --version flag.

## Command Usage

```bash
husonym version
```

The basic command will print out details such as the current Git tag, commit, build date, go version, and other relevant operating system information.

This can be augmented for systems by providing the `--output` flag to generate the version data in either `yaml` or `json`.

```bash
husonym version --output json
husonym version --output yaml
```

## Flag Usage

```bash
husonym --version
```

The top-level `--version` flag can be used to simply print out the Git tagged version of Husonym CLI.
For more details, it's recommended to use the `husonym version` command.
