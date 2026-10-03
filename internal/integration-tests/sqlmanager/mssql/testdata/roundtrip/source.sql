-- A source database that holds one of everything the generator reproduces. The database is
-- case-sensitive. Batches are separated by lines that hold GO alone.

CREATE SCHEMA sales;
GO
CREATE SCHEMA [Sales Ops];
GO
CREATE SCHEMA [we]]ird];
GO
CREATE SCHEMA [it's];
GO
CREATE SCHEMA [a.b];
GO
CREATE SCHEMA [select];
GO

-- Alias types: the same name in two schemas, one made of bytes, one of a decimal.
CREATE TYPE sales.Code FROM varbinary(50) NULL;
GO
CREATE TYPE [Sales Ops].Code FROM decimal(19,4) NOT NULL;
GO
CREATE TYPE sales.Email FROM nvarchar(320) NOT NULL;
GO
CREATE TYPE [it's].[Counter ]] type] FROM int NOT NULL;
GO

-- Sequences of every type, one cycling, one without cache, one going down, one typed by an
-- alias, one that has given values.
CREATE SEQUENCE [it's].[seq tiny] AS tinyint START WITH 5 INCREMENT BY 5 MINVALUE 0 MAXVALUE 250 CYCLE CACHE 10;
GO
CREATE SEQUENCE sales.seq_small AS smallint START WITH 100 INCREMENT BY 1 NO CACHE;
GO
CREATE SEQUENCE sales.seq_int AS int START WITH 1000 INCREMENT BY -3 MINVALUE -1000000 MAXVALUE 1000;
GO
CREATE SEQUENCE sales.seq_big AS bigint START WITH 1000 INCREMENT BY 1 MINVALUE 1000 CACHE 50;
GO
CREATE SEQUENCE sales.seq_decimal AS decimal(18,0) START WITH 1 INCREMENT BY 1;
GO
CREATE SEQUENCE sales.seq_alias AS [it's].[Counter ]] type] START WITH 7 INCREMENT BY 7;
GO
CREATE SEQUENCE [a.b].[used.seq] AS int START WITH 10 INCREMENT BY 10 CACHE 3;
GO

-- A function of another schema, called by a computed column, by a check and by a view.
CREATE FUNCTION [a.b].[double it](@n int) RETURNS int AS BEGIN RETURN @n * 2 END
GO

-- Every type, with lengths that are not the default.
CREATE TABLE dbo.all_types (
    c_bigint bigint NOT NULL,
    c_int int NULL,
    c_smallint smallint NULL,
    c_tinyint tinyint NULL,
    c_bit bit NOT NULL,
    c_money money NULL,
    c_smallmoney smallmoney NULL,
    c_decimal decimal(38,10) NULL,
    c_numeric numeric(5,0) NOT NULL,
    c_float24 float(24) NULL,
    c_float53 float(53) NULL,
    c_char char(7) NULL,
    c_varchar varchar(33) NOT NULL,
    c_varchar_max varchar(max) NULL,
    c_nchar nchar(10) NULL,
    c_nvarchar nvarchar(50) NULL,
    c_nvarchar_max nvarchar(max) NULL,
    c_binary binary(10) NULL,
    c_varbinary varbinary(20) NULL,
    c_varbinary_max varbinary(max) NULL,
    c_date date NULL,
    c_datetime datetime NULL,
    c_smalldatetime smalldatetime NULL,
    c_time0 time(0) NULL,
    c_time7 time(7) NULL,
    c_datetime2 datetime2(3) NULL,
    c_datetimeoffset datetimeoffset(5) NULL,
    c_guid uniqueidentifier ROWGUIDCOL NOT NULL CONSTRAINT [DF guid] DEFAULT (newsequentialid()),
    c_xml xml NULL,
    c_variant sql_variant NULL,
    c_hierarchy hierarchyid NULL,
    c_geometry geometry NULL,
    c_geography geography NULL,
    c_text text NULL,
    c_ntext ntext NULL,
    c_image image NULL,
    c_rowversion rowversion NOT NULL,
    c_sysname sysname NOT NULL,
    c_other_collation varchar(20) COLLATE Latin1_General_100_CI_AI NULL,
    c_code sales.Code NULL,
    c_amount [Sales Ops].Code NOT NULL,
    c_email sales.Email NOT NULL
);
GO

-- Names that need every escape. The two last columns differ by their case alone.
CREATE TABLE [we]]ird].[Order ]] Lines] (
    [id] int NOT NULL,
    [it's] nvarchar(40) NOT NULL,
    [a.b] int NULL,
    [select] int NULL,
    [x, y] int NULL,
    [MixedCase] int NULL,
    [mixedcase] int NULL,
    CONSTRAINT [PK ]] lines] PRIMARY KEY CLUSTERED ([id]),
    CONSTRAINT [UQ it's] UNIQUE ([it's], [x, y] DESC),
    CONSTRAINT [CK a.b] CHECK ([a.b] > 0 OR [select] IS NULL)
);
GO
CREATE INDEX [IX x, y] ON [we]]ird].[Order ]] Lines] ([MixedCase], [mixedcase] DESC) INCLUDE ([a.b], [select]);
GO

-- Identities.
CREATE TABLE sales.id_default (id int IDENTITY(1,1) NOT NULL, v int NULL);
GO
CREATE TABLE sales.id_odd (id bigint IDENTITY(-5,10) NOT NULL, v int NULL);
GO
CREATE TABLE sales.id_decimal (
    id decimal(20,0) IDENTITY(10000000000000000000,1) NOT FOR REPLICATION NOT NULL,
    v int NULL
);
GO

-- Computed columns.
CREATE TABLE sales.computed (
    a int NULL,
    b int NULL,
    first_name nvarchar(20) NOT NULL,
    last_name nvarchar(20) NOT NULL,
    plain AS (a + b),
    stored AS (a * b) PERSISTED,
    stored_not_null AS (ISNULL(a, 0)) PERSISTED NOT NULL,
    full_name AS (first_name + N' ' + last_name),
    doubled AS ([a.b].[double it](a))
);
GO

-- Defaults: named, named by the server, drawing from a sequence of another schema, holding a quote.
CREATE TABLE sales.defaults (
    id int NOT NULL CONSTRAINT DF_defaults_id DEFAULT (NEXT VALUE FOR [a.b].[used.seq]),
    named int NOT NULL CONSTRAINT [DF named] DEFAULT ((42)),
    unnamed datetime2(0) NOT NULL DEFAULT (sysutcdatetime()),
    quoted nvarchar(20) NOT NULL CONSTRAINT DF_quoted DEFAULT (N'it''s'),
    tiny tinyint NOT NULL CONSTRAINT DF_tiny DEFAULT (NEXT VALUE FOR [it's].[seq tiny]),
    small smallint NOT NULL CONSTRAINT DF_small DEFAULT (NEXT VALUE FOR sales.seq_small),
    medium int NOT NULL CONSTRAINT DF_medium DEFAULT (NEXT VALUE FOR sales.seq_int),
    big bigint NOT NULL CONSTRAINT DF_big DEFAULT (NEXT VALUE FOR sales.seq_big),
    dec decimal(18,0) NOT NULL CONSTRAINT DF_dec DEFAULT (NEXT VALUE FOR sales.seq_decimal),
    aliased int NOT NULL CONSTRAINT DF_aliased DEFAULT (NEXT VALUE FOR sales.seq_alias)
);
GO
INSERT INTO sales.defaults (named) VALUES (1), (2), (3);
GO

-- Sparse columns with their column set, and masked columns.
CREATE TABLE [Sales Ops].[sparse and masked] (
    id int NOT NULL,
    s1 int SPARSE NULL,
    s2 varchar(10) SPARSE NULL,
    everything xml COLUMN_SET FOR ALL_SPARSE_COLUMNS,
    card varchar(30) MASKED WITH (FUNCTION = 'partial(1, "xx''x", 1)') NULL,
    secret int MASKED WITH (FUNCTION = 'default()') NOT NULL
);
GO

-- Keys.
CREATE TABLE sales.pk_two (
    a int NOT NULL,
    b int NOT NULL,
    v int NULL,
    CONSTRAINT PK_two PRIMARY KEY CLUSTERED (a DESC, b ASC)
);
GO
CREATE TABLE sales.pk_nonclustered (
    id int NOT NULL,
    v int NOT NULL,
    CONSTRAINT PK_nonclustered PRIMARY KEY NONCLUSTERED (id) WITH (FILLFACTOR = 90)
);
GO
CREATE CLUSTERED INDEX CIX_nonclustered ON sales.pk_nonclustered (v DESC);
GO
CREATE TABLE sales.heap (a int NULL, b varchar(10) NULL);
GO
CREATE TABLE sales.pk_system_named (id int NOT NULL PRIMARY KEY, v int NULL UNIQUE);
GO

-- One table with three unique constraints, four checks and three foreign keys.
CREATE TABLE sales.busy (
    id int NOT NULL CONSTRAINT PK_busy PRIMARY KEY,
    u1 int NOT NULL,
    u2 int NOT NULL,
    u3 int NOT NULL,
    two_a int NULL,
    two_b int NULL,
    parent_id int NULL,
    line_id int NULL DEFAULT ((0)),
    qty int NULL CHECK (qty >= 0),
    price int NULL,
    CONSTRAINT UQ_busy_1 UNIQUE (u1),
    CONSTRAINT UQ_busy_2 UNIQUE CLUSTERED (u2 DESC),
    CONSTRAINT UQ_busy_3 UNIQUE (u3, u1),
    CONSTRAINT CK_busy_price CHECK (price > 0),
    CONSTRAINT CK_busy_double CHECK ([a.b].[double it](qty) < 1000),
    CONSTRAINT CK_busy_off CHECK (price < 100000),
    CONSTRAINT FK_busy_two FOREIGN KEY (two_a, two_b) REFERENCES sales.pk_two (a, b),
    CONSTRAINT FK_busy_self FOREIGN KEY (parent_id) REFERENCES sales.busy (id),
    CONSTRAINT FK_busy_lines FOREIGN KEY (line_id) REFERENCES [we]]ird].[Order ]] Lines] (id)
        ON DELETE SET DEFAULT ON UPDATE CASCADE
);
GO
ALTER TABLE sales.busy NOCHECK CONSTRAINT CK_busy_off;
GO
ALTER TABLE sales.busy WITH NOCHECK ADD CONSTRAINT CK_busy_untrusted CHECK (qty < 500) ;
GO
ALTER TABLE sales.busy ADD CONSTRAINT CK_busy_replication CHECK NOT FOR REPLICATION (u1 > -1000);
GO

-- Two tables that reference each other; actions; a disabled key; a key that is not trusted.
CREATE TABLE sales.chicken (
    id int NOT NULL CONSTRAINT PK_chicken PRIMARY KEY,
    egg_id int NULL
);
GO
CREATE TABLE sales.egg (
    id int NOT NULL CONSTRAINT PK_egg PRIMARY KEY,
    chicken_id int NULL,
    other_chicken_id int NULL,
    third_chicken_id int NULL,
    fourth_chicken_id int NULL,
    CONSTRAINT FK_egg_chicken FOREIGN KEY (chicken_id) REFERENCES sales.chicken (id)
        ON DELETE CASCADE ON UPDATE SET NULL
);
GO
ALTER TABLE sales.chicken ADD CONSTRAINT FK_chicken_egg FOREIGN KEY (egg_id) REFERENCES sales.egg (id);
GO
ALTER TABLE sales.egg ADD CONSTRAINT FK_egg_disabled FOREIGN KEY (other_chicken_id) REFERENCES sales.chicken (id);
GO
ALTER TABLE sales.egg NOCHECK CONSTRAINT FK_egg_disabled;
GO
ALTER TABLE sales.egg WITH NOCHECK ADD CONSTRAINT FK_egg_untrusted FOREIGN KEY (third_chicken_id)
    REFERENCES sales.chicken (id);
GO
ALTER TABLE sales.egg ADD CONSTRAINT FK_egg_replication FOREIGN KEY (fourth_chicken_id)
    REFERENCES sales.chicken (id) NOT FOR REPLICATION;
GO

-- Indexes.
CREATE TABLE sales.indexed (
    id int NOT NULL,
    a int NOT NULL,
    b int NULL,
    c varchar(20) NULL,
    d varchar(20) NULL,
    e int NULL,
    f int NULL
);
GO
CREATE UNIQUE INDEX UX_indexed_a ON sales.indexed (a);
GO
CREATE INDEX IX_indexed_filtered ON sales.indexed (b) WHERE b IS NOT NULL AND c = 'x';
GO
CREATE INDEX IX_indexed_descending ON sales.indexed (c DESC, a ASC, b DESC);
GO
CREATE INDEX IX_indexed_included ON sales.indexed (d) INCLUDE (e, c);
GO
CREATE INDEX IX_indexed_padded ON sales.indexed (e) WITH (PAD_INDEX = ON, FILLFACTOR = 70);
GO
CREATE UNIQUE INDEX UX_indexed_ignore ON sales.indexed (f, id) WITH (IGNORE_DUP_KEY = ON);
GO
CREATE INDEX IX_indexed_locks ON sales.indexed (e, f) WITH (ALLOW_ROW_LOCKS = OFF, ALLOW_PAGE_LOCKS = OFF);
GO
CREATE INDEX IX_indexed_disabled ON sales.indexed (f);
GO
ALTER INDEX IX_indexed_disabled ON sales.indexed DISABLE;
GO
CREATE NONCLUSTERED COLUMNSTORE INDEX NCCI_indexed ON sales.indexed (a, e, f) WHERE e > 0;
GO
CREATE TABLE [select].[facts] (
    [day] date NOT NULL,
    [amount] decimal(18,2) NOT NULL,
    [select] int NULL
);
GO
CREATE CLUSTERED COLUMNSTORE INDEX CCI_facts ON [select].[facts];
GO

-- Views, functions and procedures of the schema sales.
CREATE FUNCTION sales.fn_inline(@min int) RETURNS TABLE AS RETURN (SELECT id, v FROM sales.id_default WHERE id >= @min)
GO
CREATE VIEW sales.v_base AS SELECT id, v FROM sales.fn_inline(0)
GO
CREATE VIEW sales.a_view_on_view AS SELECT id, [a.b].[double it](v) AS doubled FROM sales.v_base
GO
CREATE VIEW sales.v_bound WITH SCHEMABINDING AS SELECT a, b, v FROM sales.pk_two
GO
CREATE FUNCTION sales.fn_multi(@n int) RETURNS @t TABLE (n int NOT NULL, label nvarchar(10) NULL) AS
BEGIN
    INSERT INTO @t (n, label) VALUES (@n, N'one'), (@n + 1, N'two');
    RETURN;
END
GO
CREATE PROCEDURE sales.p_ping @depth int AS
BEGIN
    SET NOCOUNT ON;
    IF @depth > 0 EXEC sales.p_pong @depth;
END
GO
CREATE PROCEDURE sales.p_pong @depth int AS
BEGIN
    SET NOCOUNT ON;
    DECLARE @next int = @depth - 1;
    EXEC sales.p_ping @next;
END
GO
SET QUOTED_IDENTIFIER OFF;
GO
CREATE VIEW sales.v_quoted_off AS SELECT id, "a string, not a name" AS label FROM sales.id_default
GO
SET QUOTED_IDENTIFIER ON;
GO
CREATE VIEW sales.[v it's [odd]]] AS
SELECT id AS [a]]b], 'it''s' AS [quote's], N'[x]' AS "double ""quoted""", '100%' AS pct
FROM sales.id_default
GO

-- Triggers: one enabled, one disabled, one on a view.
CREATE TRIGGER sales.trg_busy_insert ON sales.busy AFTER INSERT AS
BEGIN
    SET NOCOUNT ON;
    UPDATE b SET price = b.price FROM sales.busy b JOIN inserted i ON i.id = b.id WHERE 1 = 0;
END
GO
CREATE TRIGGER sales.[trg busy ]] off] ON sales.busy AFTER DELETE AS
BEGIN
    SET NOCOUNT ON;
END
GO
DISABLE TRIGGER sales.[trg busy ]] off] ON sales.busy;
GO
CREATE TRIGGER sales.trg_view_insert ON sales.v_base INSTEAD OF INSERT AS
BEGIN
    SET NOCOUNT ON;
    INSERT INTO sales.id_default (v) SELECT v FROM inserted;
END
GO

-- Temporal tables: a period without versioning, and a versioned table whose history table is
-- named, has an index of its own and is kept six months. One period column is hidden.
CREATE TABLE [it's].[period only] (
    id int NOT NULL CONSTRAINT [PK period only] PRIMARY KEY,
    label nvarchar(20) NULL,
    valid_from datetime2(7) GENERATED ALWAYS AS ROW START NOT NULL,
    valid_to datetime2(7) GENERATED ALWAYS AS ROW END NOT NULL,
    PERIOD FOR SYSTEM_TIME (valid_from, valid_to)
);
GO
CREATE TABLE [it's].[staff] (
    id int NOT NULL CONSTRAINT PK_staff PRIMARY KEY,
    name nvarchar(50) NOT NULL,
    salary decimal(10,2) NULL,
    valid_from datetime2(3) GENERATED ALWAYS AS ROW START NOT NULL,
    valid_to datetime2(3) GENERATED ALWAYS AS ROW END HIDDEN NOT NULL,
    PERIOD FOR SYSTEM_TIME (valid_from, valid_to)
) WITH (SYSTEM_VERSIONING = ON (HISTORY_TABLE = [it's].[staff history], HISTORY_RETENTION_PERIOD = 6 MONTHS));
GO
CREATE INDEX [IX staff history name] ON [it's].[staff history] (name);
GO

-- What the generator leaves out and reports: partitioning, compression, extended properties,
-- a grant.
CREATE PARTITION FUNCTION pf_region (int) AS RANGE LEFT FOR VALUES (100, 200);
GO
CREATE PARTITION SCHEME ps_region AS PARTITION pf_region ALL TO ([PRIMARY]);
GO
CREATE TABLE sales.partitioned (
    id int NOT NULL,
    region int NOT NULL,
    v int NULL,
    CONSTRAINT PK_partitioned PRIMARY KEY CLUSTERED (id, region)
) ON ps_region (region);
GO
CREATE INDEX IX_partitioned_v ON sales.partitioned (v);
GO
CREATE TABLE sales.compressed (id int NOT NULL, v varchar(100) NULL) WITH (DATA_COMPRESSION = PAGE);
GO
CREATE TABLE sales.described (id int NOT NULL, v int NULL);
GO
EXEC sys.sp_addextendedproperty @name = N'MS_Description', @value = N'A described table',
    @level0type = N'SCHEMA', @level0name = N'sales', @level1type = N'TABLE', @level1name = N'described';
GO
EXEC sys.sp_addextendedproperty @name = N'MS_Description', @value = N'A described column',
    @level0type = N'SCHEMA', @level0name = N'sales', @level1type = N'TABLE', @level1name = N'described',
    @level2type = N'COLUMN', @level2name = N'v';
GO
GRANT SELECT ON sales.described TO public;
GO
