---
title: Column Removal Strategies
description: Learn how to configure Husonym to handle old columns that no longer exist in the source database
id: column-removal-strategies
hide_title: false
slug: /guides/column-removal-strategies
---

## Introduction

When a Husonym Job is configured for relational databases, all columns for each selected table must have a transformer mapping configured.
This page goes into detail how Husonym handles old columns that no longer exist in the source database and the different strategies that can be used to handle this.

This is a common occurrence for any company that is adding or removing columns to a database and may not update Husonym straight away.

The `continue` strategy is the default strategy as it is the most flexible. The idea is to keep Husonym running and being less brittle to configuration drift without having to constantly check in on how Husonym is doing.

## Driver Support

| Strategy | Description                                                                                                                                                                                          | PostgreSQL | MySQL | MS SQL Server |
| -------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------- | ----- | ------------- |
| Halt     | Stops the job run if a column is configured in the job mappings but no longer exists in the source database.                                                                                         | ✅         | ✅    | ✅            |
| Continue | Columns are ignored from the source and are not inserted into the destination; This may fail if column still exists in the destination but does not have a default value configured. See more below. | ✅         | ✅    | ✅            |

## Halt Strategy

This strategy is plain and simple. During the job run, Husonym compares the configured job mappings with the source database.
For the selected tables in the job mappings, a diff is made and if a column is found in the job mappings that doesn't exist in the source database, the run is halted.

## Continue Strategy

This strategy tells Husonym to ignore any difference in job mappings from the source database.

Husonym is able to detect that columns were removed in the source, and will leave them off of the insert statement.
This may result in failures if any unmapped columns do not have a column default in the destination connection.
