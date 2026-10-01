---
title: Trigger
description: Learn how to trigger a Husonym job with the husonym jobs trigger command.
id: trigger
hide_title: false
slug: /cli/jobs/trigger
---

## Overview

Learn how to trigger a Husonym job with the husonym jobs trigger command.

The `husonym jobs trigger` command is used to trigger an execution of a Husonym job.
This is useful if a Job is configured but is not running on a schedule, or it's desired to trigger a job outside of the normal scheduled flow.

## Usage

```bash
husonym jobs trigger <job-id>
```

### Argument: job-id

A job-id must be provided as the first command-line argument. This is required and will fail otherwise.
This job-id is used to trigger a workflow execution of the relevant Husonym Job.

## Output

The command prints the id of the run it started, alone on its output:

```bash
run_id=$(husonym jobs trigger <job-id>)
```

The command returns once the run has started, without waiting for it to end. It fails, and starts nothing, while a run of the job is in progress. Against an API that does not name the run it starts, it prints nothing and warns on its error output.
