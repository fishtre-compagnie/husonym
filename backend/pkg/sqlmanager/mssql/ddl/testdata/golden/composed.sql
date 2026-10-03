-- schemas
IF NOT EXISTS (SELECT 1 FROM sys.schemas WHERE name = N'core')
EXEC (N'CREATE SCHEMA [core]')
GO
IF NOT EXISTS (SELECT 1 FROM sys.schemas WHERE name = N'hr')
EXEC (N'CREATE SCHEMA [hr]')
GO
IF NOT EXISTS (SELECT 1 FROM sys.schemas WHERE name = N'sales')
EXEC (N'CREATE SCHEMA [sales]')
GO
IF NOT EXISTS (SELECT 1 FROM sys.schemas WHERE name = N'util')
EXEC (N'CREATE SCHEMA [util]')
GO
-- data types
-- skipped: [sales].[InvoiceNo]: created at its declared start 1000; the source is at 1042
IF TYPE_ID(N'[sales].[EmailAddress]') IS NULL
CREATE TYPE [sales].[EmailAddress] FROM nvarchar(320) NOT NULL
GO
IF OBJECT_ID(N'[sales].[InvoiceNo]', N'SO') IS NULL
CREATE SEQUENCE [sales].[InvoiceNo] AS bigint START WITH 1000 INCREMENT BY 1 MINVALUE 1000 MAXVALUE 9223372036854775807 NO CYCLE CACHE 50
GO
IF OBJECT_ID(N'[util].[positive]', N'FN') IS NULL
BEGIN
    EXEC (N'SET ANSI_NULLS ON; SET QUOTED_IDENTIFIER ON; EXEC (N''CREATE FUNCTION util.positive(@n decimal(9,2)) RETURNS bit AS BEGIN RETURN IIF(@n > 0, 1, 0) END'')');
    IF OBJECT_ID(N'[util].[positive]', N'FN') IS NULL
        THROW 50000, N'the definition of function [util].[positive] did not create it under that name', 1;
END
GO
-- create table
-- skipped: [sales].[Gone]: not found in the source database
-- skipped: [sales].[Order ]] Lines]: collation of 1 alias-typed column(s) follows the default of the destination database
-- skipped: [sales].[Order ]] Lines]: compression not reproduced: PAGE
-- skipped: [core].[Orders]: extended properties not reproduced: 2
IF OBJECT_ID(N'[hr].[staff_history]', N'U') IS NULL
CREATE TABLE [hr].[staff_history] (
    [id] int NOT NULL,
    [name] nvarchar(50) COLLATE Latin1_General_100_CI_AS NOT NULL,
    [valid_from] datetime2(7) NOT NULL,
    [valid_to] datetime2(7) NOT NULL
)
GO
IF OBJECT_ID(N'[sales].[Order ]] Lines]', N'U') IS NULL
CREATE TABLE [sales].[Order ]] Lines] (
    [id] int IDENTITY(1,1) NOT NULL,
    [it's] nvarchar(40) COLLATE Latin1_General_100_CI_AS NOT NULL,
    [note] varchar(max) COLLATE Latin1_General_100_CI_AS NULL,
    [email] [sales].[EmailAddress] NOT NULL,
    [qty] decimal(9,2) CONSTRAINT [DF_lines_qty] DEFAULT (NEXT VALUE FOR [sales].[InvoiceNo]) NOT NULL,
    [total] AS ([qty]*(2)) PERSISTED NOT NULL,
    [order id] int NOT NULL,
    [region] int NOT NULL
)
GO
IF OBJECT_ID(N'[core].[Orders]', N'U') IS NULL
CREATE TABLE [core].[Orders] (
    [id] int NOT NULL,
    [region] int NOT NULL
)
GO
IF OBJECT_ID(N'[hr].[staff]', N'U') IS NULL
CREATE TABLE [hr].[staff] (
    [id] int NOT NULL,
    [name] nvarchar(50) COLLATE Latin1_General_100_CI_AS NOT NULL,
    [valid_from] datetime2(7) GENERATED ALWAYS AS ROW START HIDDEN NOT NULL,
    [valid_to] datetime2(7) GENERATED ALWAYS AS ROW END HIDDEN NOT NULL,
    PERIOD FOR SYSTEM_TIME ([valid_from], [valid_to])
)
GO
-- view and functions
-- skipped: [core].[p_secret]: encrypted: its definition cannot be read
-- skipped: [sales].[v_products]: depends on table [catalog].[products], which is outside the selection
IF OBJECT_ID(N'[sales].[v_open]', N'V') IS NULL
BEGIN
    EXEC (N'SET ANSI_NULLS ON; SET QUOTED_IDENTIFIER ON; EXEC (N''CREATE VIEW [sales].[v_open] AS SELECT id FROM sales.[Order ]] Lines] WHERE qty > 0'')');
    IF OBJECT_ID(N'[sales].[v_open]', N'V') IS NULL
        THROW 50000, N'the definition of view [sales].[v_open] did not create it under that name', 1;
END
GO
IF OBJECT_ID(N'[sales].[a_summary]', N'V') IS NULL
BEGIN
    EXEC (N'SET ANSI_NULLS ON; SET QUOTED_IDENTIFIER ON; EXEC (N''CREATE VIEW sales.a_summary AS SELECT COUNT(*) AS n FROM sales.v_open'')');
    IF OBJECT_ID(N'[sales].[a_summary]', N'V') IS NULL
        THROW 50000, N'the definition of view [sales].[a_summary] did not create it under that name', 1;
END
GO
-- non-fk alter table
IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE name = N'PK_lines' AND parent_object_id = OBJECT_ID(N'[sales].[Order ]] Lines]', N'U'))
ALTER TABLE [sales].[Order ]] Lines] ADD CONSTRAINT [PK_lines] PRIMARY KEY CLUSTERED ([id] ASC) WITH (FILLFACTOR = 90)
GO
IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE name = N'CK_lines_qty' AND parent_object_id = OBJECT_ID(N'[sales].[Order ]] Lines]', N'U'))
ALTER TABLE [sales].[Order ]] Lines] WITH CHECK ADD CONSTRAINT [CK_lines_qty] CHECK ([util].[positive]([qty])=(1))
GO
IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE name = N'PK_Orders' AND parent_object_id = OBJECT_ID(N'[core].[Orders]', N'U'))
ALTER TABLE [core].[Orders] ADD CONSTRAINT [PK_Orders] PRIMARY KEY CLUSTERED ([id] ASC, [region] ASC)
GO
IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE name = N'PK_staff' AND parent_object_id = OBJECT_ID(N'[hr].[staff]', N'U'))
ALTER TABLE [hr].[staff] ADD CONSTRAINT [PK_staff] PRIMARY KEY CLUSTERED ([id] ASC)
GO
-- table index
-- skipped: [sales].[Order ]] Lines].[XI_lines]: XML indexes are not reproduced
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'ix_staff_history' AND object_id = OBJECT_ID(N'[hr].[staff_history]', N'U'))
CREATE CLUSTERED INDEX [ix_staff_history] ON [hr].[staff_history] ([valid_to] ASC, [valid_from] ASC)
GO
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'ix_staff_history_name' AND object_id = OBJECT_ID(N'[hr].[staff_history]', N'U'))
CREATE NONCLUSTERED INDEX [ix_staff_history_name] ON [hr].[staff_history] ([name] ASC)
GO
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'UX_lines_sku' AND object_id = OBJECT_ID(N'[sales].[Order ]] Lines]', N'U'))
CREATE UNIQUE NONCLUSTERED INDEX [UX_lines_sku] ON [sales].[Order ]] Lines] ([it's] ASC)
GO
IF EXISTS (SELECT 1 FROM sys.tables WHERE object_id = OBJECT_ID(N'[hr].[staff]', N'U') AND temporal_type = 0)
ALTER TABLE [hr].[staff] SET (SYSTEM_VERSIONING = ON (HISTORY_TABLE = [hr].[staff_history], HISTORY_RETENTION_PERIOD = 6 MONTHS))
GO
-- fk alter table
-- skipped: [sales].[Order ]] Lines].[FK_lines_product]: foreign key to [catalog].[products], which is not among the requested tables
IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE name = N'FK_lines_order' AND parent_object_id = OBJECT_ID(N'[sales].[Order ]] Lines]', N'U'))
ALTER TABLE [sales].[Order ]] Lines] WITH CHECK ADD CONSTRAINT [FK_lines_order] FOREIGN KEY ([order id], [region]) REFERENCES [core].[Orders] ([id], [region]) ON DELETE CASCADE
GO
-- table triggers
IF NOT EXISTS (SELECT 1 FROM sys.triggers WHERE name = N'trg_audit' AND parent_id = OBJECT_ID(N'[sales].[Order ]] Lines]'))
BEGIN
    EXEC (N'SET ANSI_NULLS ON; SET QUOTED_IDENTIFIER ON; EXEC (N''CREATE TRIGGER sales.trg_audit ON sales.[Order ]] Lines] AFTER INSERT AS RETURN'')');
    IF NOT EXISTS (SELECT 1 FROM sys.triggers WHERE name = N'trg_audit' AND parent_id = OBJECT_ID(N'[sales].[Order ]] Lines]'))
        THROW 50000, N'the definition of trigger [sales].[trg_audit] did not create it under that name', 1;
END
GO
IF EXISTS (SELECT 1 FROM sys.triggers WHERE name = N'trg_audit' AND parent_id = OBJECT_ID(N'[sales].[Order ]] Lines]') AND is_disabled = 0)
DISABLE TRIGGER [sales].[trg_audit] ON [sales].[Order ]] Lines]
GO
IF NOT EXISTS (SELECT 1 FROM sys.triggers WHERE name = N'trg_view' AND parent_id = OBJECT_ID(N'[sales].[v_open]'))
BEGIN
    EXEC (N'SET ANSI_NULLS ON; SET QUOTED_IDENTIFIER ON; EXEC (N''CREATE TRIGGER sales.trg_view ON sales.v_open INSTEAD OF INSERT AS RETURN'')');
    IF NOT EXISTS (SELECT 1 FROM sys.triggers WHERE name = N'trg_view' AND parent_id = OBJECT_ID(N'[sales].[v_open]'))
        THROW 50000, N'the definition of trigger [sales].[trg_view] did not create it under that name', 1;
END
GO
