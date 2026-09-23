---
title: New Column Addition Strategies
description: Learn how to configure Husonym to handle new columns detected during a job run
id: new-column-addition-strategies
hide_title: false
slug: /guides/new-column-addition-strategies
# cSpell:words Automap
---

## Introduction

When a Husonym Job is configured for relational databases, all columns for each selected table must have a transformer mapping configured.
This page goes into detail how Husonym handles new columns that may be added to your source database in-between updating a job.

This is a common occurrence for any company that is adding new columns to a database and may not update Husonym straight away.

## Driver Support

| Strategy         | Description                                                                                        | PostgreSQL | MySQL | MS SQL Server |
| ---------------- | -------------------------------------------------------------------------------------------------- | ---------- | ----- | ------------- |
| Halt             | Stops the job run if a new column is detected that is not found in the configured job mappings.    | ✅         | ✅    | ✅            |
| AutoMap & Review | Maps the new column as suggested, or copies it as is when nothing applies, and reports the change. | ✅         | ✅    | ✅            |
| Passthrough      | Copies the new column as is, silently. See more below.                                             | ✅         | ✅    | ✅            |
| Continue         | Ignores new columns; may fail if column doesn't have default. See more below.                      | ✅         | ✅    | ✅            |

## The job follows its source

With **AutoMap & Review** and **Passthrough**, the run that finds a new column writes the mapping it chose into the job. A new column is decided once: the next runs find it mapped, and the job's source page shows what it is mapped with, ready to be changed. A mapping somebody sets by hand in the meantime is never overwritten.

Likewise, when a column disappears from the source, the run removes its mapping from the job — unless the job's [column removal strategy](/guides/column-removal-strategies) is **Halt**.

## Halt Strategy

This strategy is plain and simple. During the job run, Husonym compares the configured job mappings with the source database.
For the selected tables in the job mappings, a diff is made and if a column is found in the source connection that doesn't exist in the job mappings, the run is halted.

## Continue Strategy

This strategy tells Husonym to ignore any difference in job mappings from the source database.

Husonym is able to detect that new columns were added in the source, but it will leave them off of the insert statement.
This may result in failures if any unmapped columns do not have a column default in the destination connection.
However, any additional columns that have a default or are a generated column will not result in a job run failure.

| Example                                                                                                 | Success |
| ------------------------------------------------------------------------------------------------------- | ------- |
| `ALTER TABLE ADD COLUMN foo TEXT NOT NULL DEFAULT "test"`                                               | ✅      |
| `ALTER TABLE ADD COLUMN foo TEXT NULL DEFAULT NULL`                                                     | ✅      |
| `ALTER TABLE ADD COLUMN full_name TEXT GENERATED ALWAYS AS (first_name \|\| ' ' \|\| last_name) STORED` | ✅      |
| `ALTER TABLE ADD COLUMN foo TEXT NOT NULL`                                                              | ❌      |

## AutoMap & Review

This strategy maps a new column the way the product would suggest it, and never halts a run:

1. a column the database computes itself — a generated column — keeps its database default, its value being recomputed by the destination;
2. a column covered by a primary key, a foreign key or a unique constraint is copied as is: a transformer there could break the constraint and fail the run;
3. a column whose name and type the PII detection recognizes is mapped with the suggested transformer, in the configuration it starts with in the catalogue — a phone number keeps its prefix, separators and length, for instance;
4. any other column is copied as is.

The detection reads names and types, never the data. A column it does not recognize — `notes`, `champ_libre` — may still hold personal data, which is why every change is reported:

- the run records each change it makes to the job: the columns it mapped, with the transformer it chose; the mappings it removed with their column; and the mapped columns whose type changed since the previous run;
- a bell in the header shows, as soon as you log in, how many changes are waiting across your jobs, and the jobs list and the job's **Review** tab show them per job — columns that read as personal data and were left in clear come first;
- in the **Review** tab, each change shows the transformer the run chose and its options. Opening it brings the transformer, its options and a preview of the column before and after them together: keep the run's choice, or change it and apply. Either way, who decided, when, and an optional note saying why are recorded. Several changes can be confirmed at once, and a mapping changed on the job's source page settles its change too.

## Passthrough

Passthrough mode is a strategy that allows Husonym to handle new columns found in source tables by setting them to passthrough. This means that any new columns detected in the source database will be included in the table sync and their values will be directly copied from the source to the destination without any transformation.

This strategy is useful when you want to reduce the need for manual updates to job mappings when new columns are added to the source database.

Passthrough is silent: nothing tells anyone that a new column is being copied untransformed. For an anonymization job that is rarely what you want, since every schema change can then quietly add personal data to the destination. Prefer **AutoMap & Review** above, which maps what it recognizes and keeps every change in front of you until someone reviews it.

## Keeping the destination schema in step

A new column can only be copied into a destination table that has it. Enable the **init schema** destination option and Husonym takes care of that: on every run, it reconciles the destination with the source, on PostgreSQL and MySQL alike.

Reconciling means the destination is made to match the source, in both directions:

- tables and columns the source has and the destination lacks are created;
- columns whose type, default or nullability changed are altered;
- columns, constraints and triggers the destination has and the source no longer has are **dropped**.

Husonym's job is to keep a destination in step with its source, with sampling and anonymization on top: when the source changes, the destination follows, removals included. Anything added by hand to a destination table — a column, a constraint, a trigger — is therefore removed on the next run. Keep such additions out of the tables a job writes to.

With init schema turned off, Husonym never touches the destination schema, and it is up to you to add a new column there before the run that copies it, or the write fails.
