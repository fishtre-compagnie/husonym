---
title: Schema Initialization Overview
description: Learn how Husonym handles schema initialization
id: schema-initialization-overview
hide_title: false
slug: /schema-init/overview
# cSpell:words ROWGUIDCOL,rowstore,columnstore,filegroups
---

## Introduction

Before launching a data sync, it's essential that your destination database is prepared to handle and mirror the structure of your source. Proper schema initialization lays the foundation by establishing all necessary tables, data types, constraints, indexes, views, sequences, and triggers, ensuring a smooth and consistent data migration process.

## SQL Server Considerations

When SQL Server is the destination, schema initialization reads the catalog of the source for the tables of the job and creates on the destination what they are made of. Each object is created only if the destination does not hold it yet, so a run can be started again.

The source database must be at compatibility level 130 or more, and the login of the source connection must hold the `VIEW DEFINITION` permission on it.

### What is created

- The schemas that hold an object of the job.
- Tables with their columns in order: types with their lengths, precision and scale (`max` included), collations, nullability, identities with their seed and increment, computed columns, defaults under their name, sparse columns and column sets, masked columns, `ROWGUIDCOL`.
- The alias types of those columns, and the sequences their defaults draw from.
- Primary keys, unique constraints, check constraints and foreign keys, each under its name, with its clustering, key order, actions, and its disabled or not-trusted state.
- Rowstore and columnstore indexes with their keys, included columns, filter and options. A disabled non-clustered index is created disabled.
- Temporal tables: the period, the history table under its name with its indexes, and the retention.
- The views, functions and procedures of the schemas of the job's tables, from their stored definition, in dependency order. A function that a table calls is created before the tables.
- The triggers of the tables and of the views created, disabled ones included.

### What stops the initialization

Some objects cannot be created as they are, and creating something close would give the destination another object under the same name. The initialization fails before anything is run and names every one of them:

- Memory-optimized, graph, ledger and external tables, and FileTables.
- Columns typed by an XML schema collection or a CLR type, Always Encrypted and `FILESTREAM` columns, columns created under `ANSI_PADDING OFF`, columns or alias types with a bound rule or a bound default.
- A disabled clustered index, or a disabled index that backs a primary key or a unique constraint.
- A function called by a table when it is encrypted, CLR, or schema-bound to a table.

### What is left out

What follows is not created. Each item is recorded with the run, next to the statements that failed:

- A table of the job that the source does not have.
- A foreign key to a table that is not part of the job.
- A view, function or procedure that is encrypted or CLR, or that depends on a table outside the job, a synonym, a table type, a CLR object, or another module that is left out.
- An encrypted or CLR trigger, and the first or last firing order of a trigger.
- XML, spatial, full-text and JSON indexes, and the order of an ordered columnstore index.
- Partitioning, filegroups, compression, user statistics, extended properties, permissions, row-level security, change tracking and change data capture.
- The current value of a sequence: it is created at its declared start.
- The collation of a column typed by an alias type: such a column takes the default collation of the destination database.

A view, a function or a procedure that fails to be created is recorded and the run goes on. Any other statement that fails stops the run.
