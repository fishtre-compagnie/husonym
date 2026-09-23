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

This strategy lets the run go on when a mapped column is gone from the source.

The run leaves the column off of the insert statement and removes its mapping from the job: the job follows its source, as the destination does. Under the **AutoMap & Review** strategy for new columns, the removal is recorded, with the transformer the column had, and shows in the job's **Review** tab.
A source that shows none of the columns the job maps fails the run instead: that is the wrong database, or a connection without the rights to read its tables, and removing every mapping would empty the job.

Without init schema, this may result in failures if a removed column is still in the destination without a default value.

With the **init schema** destination option enabled, this cannot happen: every run reconciles the destination with the source, on PostgreSQL and MySQL alike, and a column the source no longer has is dropped from the destination too. See [keeping the destination schema in step](/guides/new-column-addition-strategies#keeping-the-destination-schema-in-step).
