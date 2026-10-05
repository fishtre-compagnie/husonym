-- A dump of the catalog of a database, by names. Two databases that hold the same objects give
-- the same dump. It owes nothing to the queries of the product: it reads the system views with
-- queries of its own, and reads every attribute of what the product reproduces.
--
-- What is left out, and why, is listed in the test that compares two dumps.
-- Each batch is one query; its first column names the section.

SELECT 'schema' AS section, s.name
FROM sys.schemas s
WHERE s.schema_id BETWEEN 5 AND 16383 OR s.name = 'dbo'
ORDER BY s.name COLLATE Latin1_General_BIN2;
GO

SELECT 'type' AS section, s.name AS [schema], t.name, b.name AS base_type,
    t.max_length, t.precision, t.scale, t.is_nullable, t.is_table_type, t.is_assembly_type,
    t.rule_object_id, t.default_object_id
FROM sys.types t
JOIN sys.schemas s ON s.schema_id = t.schema_id
LEFT JOIN sys.types b ON b.user_type_id = t.system_type_id
WHERE t.is_user_defined = 1
ORDER BY s.name COLLATE Latin1_General_BIN2, t.name COLLATE Latin1_General_BIN2;
GO

SELECT 'sequence' AS section, s.name AS [schema], q.name, ts.name AS type_schema, t.name AS type,
    q.precision, q.scale,
    CONVERT(nvarchar(60), q.start_value) AS start_value, CONVERT(nvarchar(60), q.increment) AS increment,
    CONVERT(nvarchar(60), q.minimum_value) AS minimum_value, CONVERT(nvarchar(60), q.maximum_value) AS maximum_value,
    q.is_cycling, q.is_cached, q.cache_size
FROM sys.sequences q
JOIN sys.schemas s ON s.schema_id = q.schema_id
JOIN sys.types t ON t.user_type_id = q.user_type_id
JOIN sys.schemas ts ON ts.schema_id = t.schema_id
ORDER BY s.name COLLATE Latin1_General_BIN2, q.name COLLATE Latin1_General_BIN2;
GO

SELECT 'table' AS section, s.name AS [schema], t.name, t.type_desc,
    t.temporal_type_desc, hs.name AS history_schema, h.name AS history_table,
    t.history_retention_period, t.history_retention_period_unit_desc,
    t.is_memory_optimized, t.durability_desc, t.lock_escalation_desc, t.uses_ansi_nulls,
    t.large_value_types_out_of_row, t.text_in_row_limit, t.is_filetable, t.is_external, t.is_node, t.is_edge,
    t.ledger_type_desc, t.is_replicated, t.lock_on_bulk_load
FROM sys.tables t
JOIN sys.schemas s ON s.schema_id = t.schema_id
LEFT JOIN sys.tables h ON h.object_id = t.history_table_id
LEFT JOIN sys.schemas hs ON hs.schema_id = h.schema_id
WHERE t.is_ms_shipped = 0
ORDER BY s.name COLLATE Latin1_General_BIN2, t.name COLLATE Latin1_General_BIN2;
GO

SELECT 'period' AS section, s.name AS [schema], t.name AS [table], p.name, p.period_type_desc,
    cs.name AS start_column, ce.name AS end_column
FROM sys.periods p
JOIN sys.tables t ON t.object_id = p.object_id
JOIN sys.schemas s ON s.schema_id = t.schema_id
JOIN sys.columns cs ON cs.object_id = p.object_id AND cs.column_id = p.start_column_id
JOIN sys.columns ce ON ce.object_id = p.object_id AND ce.column_id = p.end_column_id
ORDER BY s.name COLLATE Latin1_General_BIN2, t.name COLLATE Latin1_General_BIN2;
GO

-- The collation of a column typed by an alias follows the database it is created in: it is
-- shown as such.
SELECT 'column' AS section, s.name AS [schema], t.name AS [table], c.column_id, c.name,
    ts.name AS type_schema, ty.name AS type, c.max_length, c.precision, c.scale,
    CASE WHEN ty.is_user_defined = 1 AND c.collation_name IS NOT NULL THEN '(of the database)'
         ELSE c.collation_name END AS collation_name,
    c.is_nullable, c.is_ansi_padded, c.is_rowguidcol, c.is_identity, c.is_computed, c.is_filestream,
    c.is_replicated, c.is_xml_document, c.xml_collection_id, c.rule_object_id,
    CASE WHEN c.default_object_id = 0 THEN 0 ELSE 1 END AS has_default,
    c.is_sparse, c.is_column_set, c.generated_always_type_desc, c.encryption_type_desc, c.is_hidden,
    c.is_masked, c.graph_type_desc
FROM sys.columns c
JOIN sys.tables t ON t.object_id = c.object_id
JOIN sys.schemas s ON s.schema_id = t.schema_id
JOIN sys.types ty ON ty.user_type_id = c.user_type_id
JOIN sys.schemas ts ON ts.schema_id = ty.schema_id
WHERE t.is_ms_shipped = 0
ORDER BY s.name COLLATE Latin1_General_BIN2, t.name COLLATE Latin1_General_BIN2, c.column_id;
GO

SELECT 'default' AS section, s.name AS [schema], t.name AS [table], c.name AS [column], d.name, d.definition
FROM sys.default_constraints d
JOIN sys.tables t ON t.object_id = d.parent_object_id
JOIN sys.schemas s ON s.schema_id = t.schema_id
JOIN sys.columns c ON c.object_id = d.parent_object_id AND c.column_id = d.parent_column_id
WHERE t.is_ms_shipped = 0
ORDER BY s.name COLLATE Latin1_General_BIN2, t.name COLLATE Latin1_General_BIN2, d.name COLLATE Latin1_General_BIN2;
GO

SELECT 'computed' AS section, s.name AS [schema], t.name AS [table], c.name, c.definition,
    c.is_persisted, c.uses_database_collation
FROM sys.computed_columns c
JOIN sys.tables t ON t.object_id = c.object_id
JOIN sys.schemas s ON s.schema_id = t.schema_id
WHERE t.is_ms_shipped = 0
ORDER BY s.name COLLATE Latin1_General_BIN2, t.name COLLATE Latin1_General_BIN2, c.column_id;
GO

SELECT 'identity' AS section, s.name AS [schema], t.name AS [table], c.name,
    CONVERT(nvarchar(60), c.seed_value) AS seed_value, CONVERT(nvarchar(60), c.increment_value) AS increment_value,
    c.is_not_for_replication
FROM sys.identity_columns c
JOIN sys.tables t ON t.object_id = c.object_id
JOIN sys.schemas s ON s.schema_id = t.schema_id
WHERE t.is_ms_shipped = 0
ORDER BY s.name COLLATE Latin1_General_BIN2, t.name COLLATE Latin1_General_BIN2;
GO

SELECT 'mask' AS section, s.name AS [schema], t.name AS [table], c.name, c.masking_function
FROM sys.masked_columns c
JOIN sys.tables t ON t.object_id = c.object_id
JOIN sys.schemas s ON s.schema_id = t.schema_id
WHERE t.is_ms_shipped = 0
ORDER BY s.name COLLATE Latin1_General_BIN2, t.name COLLATE Latin1_General_BIN2, c.column_id;
GO

SELECT 'index' AS section, s.name AS [schema], t.name AS [table], i.name, i.type_desc,
    i.is_unique, i.is_primary_key, i.is_unique_constraint, i.is_disabled, i.is_hypothetical,
    i.is_padded, i.fill_factor, i.ignore_dup_key, i.allow_row_locks, i.allow_page_locks,
    i.has_filter, i.filter_definition, i.optimize_for_sequential_key, i.auto_created
FROM sys.indexes i
JOIN sys.tables t ON t.object_id = i.object_id
JOIN sys.schemas s ON s.schema_id = t.schema_id
WHERE t.is_ms_shipped = 0 AND i.type > 0
ORDER BY s.name COLLATE Latin1_General_BIN2, t.name COLLATE Latin1_General_BIN2, i.name COLLATE Latin1_General_BIN2;
GO

-- A column that is neither a key nor included is one the server added to the index of a
-- partitioned table: partitioning is not reproduced, and neither is that column.
SELECT 'index column' AS section, s.name AS [schema], t.name AS [table], i.name AS [index],
    ic.key_ordinal, c.name, ic.is_descending_key, ic.is_included_column
FROM sys.index_columns ic
JOIN sys.indexes i ON i.object_id = ic.object_id AND i.index_id = ic.index_id
JOIN sys.tables t ON t.object_id = i.object_id
JOIN sys.schemas s ON s.schema_id = t.schema_id
JOIN sys.columns c ON c.object_id = ic.object_id AND c.column_id = ic.column_id
WHERE t.is_ms_shipped = 0 AND (ic.key_ordinal > 0 OR ic.is_included_column = 1)
ORDER BY s.name COLLATE Latin1_General_BIN2, t.name COLLATE Latin1_General_BIN2, i.name COLLATE Latin1_General_BIN2,
    ic.key_ordinal, c.column_id;
GO

SELECT 'key' AS section, s.name AS [schema], t.name AS [table], k.name, k.type_desc, i.name AS [index]
FROM sys.key_constraints k
JOIN sys.tables t ON t.object_id = k.parent_object_id
JOIN sys.schemas s ON s.schema_id = t.schema_id
JOIN sys.indexes i ON i.object_id = k.parent_object_id AND i.index_id = k.unique_index_id
WHERE t.is_ms_shipped = 0
ORDER BY s.name COLLATE Latin1_General_BIN2, t.name COLLATE Latin1_General_BIN2, k.name COLLATE Latin1_General_BIN2;
GO

SELECT 'foreign key' AS section, s.name AS [schema], t.name AS [table], f.name,
    rs.name AS referenced_schema, rt.name AS referenced_table, ri.name AS referenced_key,
    f.delete_referential_action_desc, f.update_referential_action_desc,
    f.is_disabled, f.is_not_trusted, f.is_not_for_replication
FROM sys.foreign_keys f
JOIN sys.tables t ON t.object_id = f.parent_object_id
JOIN sys.schemas s ON s.schema_id = t.schema_id
JOIN sys.tables rt ON rt.object_id = f.referenced_object_id
JOIN sys.schemas rs ON rs.schema_id = rt.schema_id
LEFT JOIN sys.indexes ri ON ri.object_id = f.referenced_object_id AND ri.index_id = f.key_index_id
ORDER BY s.name COLLATE Latin1_General_BIN2, t.name COLLATE Latin1_General_BIN2, f.name COLLATE Latin1_General_BIN2;
GO

SELECT 'foreign key column' AS section, s.name AS [schema], t.name AS [table], f.name AS [key],
    fc.constraint_column_id, pc.name AS [column], rc.name AS referenced_column
FROM sys.foreign_key_columns fc
JOIN sys.foreign_keys f ON f.object_id = fc.constraint_object_id
JOIN sys.tables t ON t.object_id = f.parent_object_id
JOIN sys.schemas s ON s.schema_id = t.schema_id
JOIN sys.columns pc ON pc.object_id = fc.parent_object_id AND pc.column_id = fc.parent_column_id
JOIN sys.columns rc ON rc.object_id = fc.referenced_object_id AND rc.column_id = fc.referenced_column_id
ORDER BY s.name COLLATE Latin1_General_BIN2, t.name COLLATE Latin1_General_BIN2, f.name COLLATE Latin1_General_BIN2,
    fc.constraint_column_id;
GO

SELECT 'check' AS section, s.name AS [schema], t.name AS [table], k.name, k.definition,
    k.is_disabled, k.is_not_trusted, k.is_not_for_replication, k.uses_database_collation
FROM sys.check_constraints k
JOIN sys.tables t ON t.object_id = k.parent_object_id
JOIN sys.schemas s ON s.schema_id = t.schema_id
WHERE t.is_ms_shipped = 0
ORDER BY s.name COLLATE Latin1_General_BIN2, t.name COLLATE Latin1_General_BIN2, k.name COLLATE Latin1_General_BIN2;
GO

SELECT 'module' AS section, s.name AS [schema], o.name, RTRIM(o.type) AS type, m.definition,
    m.uses_ansi_nulls, m.uses_quoted_identifier, m.is_schema_bound, m.uses_database_collation,
    m.is_recompiled, m.null_on_null_input, m.execute_as_principal_id, m.uses_native_compilation
FROM sys.sql_modules m
JOIN sys.objects o ON o.object_id = m.object_id
JOIN sys.schemas s ON s.schema_id = o.schema_id
WHERE o.is_ms_shipped = 0
ORDER BY s.name COLLATE Latin1_General_BIN2, o.name COLLATE Latin1_General_BIN2;
GO

SELECT 'trigger' AS section, s.name AS [schema], p.name AS parent, tr.name, tr.type_desc,
    tr.is_disabled, tr.is_instead_of_trigger, tr.is_not_for_replication
FROM sys.triggers tr
JOIN sys.objects p ON p.object_id = tr.parent_id
JOIN sys.schemas s ON s.schema_id = p.schema_id
WHERE tr.parent_class = 1 AND tr.is_ms_shipped = 0
ORDER BY s.name COLLATE Latin1_General_BIN2, p.name COLLATE Latin1_General_BIN2, tr.name COLLATE Latin1_General_BIN2;
GO
