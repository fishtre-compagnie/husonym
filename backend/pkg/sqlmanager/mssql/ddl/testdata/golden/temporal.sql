-- schemas
IF NOT EXISTS (SELECT 1 FROM sys.schemas WHERE name = N'archive')
EXEC (N'CREATE SCHEMA [archive]')
GO
IF NOT EXISTS (SELECT 1 FROM sys.schemas WHERE name = N'hr')
EXEC (N'CREATE SCHEMA [hr]')
GO
-- data types
-- create table
IF OBJECT_ID(N'[hr].[staff_history]', N'U') IS NULL
CREATE TABLE [hr].[staff_history] (
    [id] int NOT NULL,
    [name] nvarchar(50) COLLATE Latin1_General_100_CI_AS NOT NULL,
    [valid_from] datetime2(7) NOT NULL,
    [valid_to] datetime2(7) NOT NULL
)
GO
IF OBJECT_ID(N'[archive].[kept ]] history]', N'U') IS NULL
CREATE TABLE [archive].[kept ]] history] (
    [id] int NULL
)
GO
IF OBJECT_ID(N'[hr].[one day history]', N'U') IS NULL
CREATE TABLE [hr].[one day history] (
    [id] int NULL
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
IF OBJECT_ID(N'[hr].[kept for ever]', N'U') IS NULL
CREATE TABLE [hr].[kept for ever] (
    [id] int NULL
)
GO
IF OBJECT_ID(N'[hr].[one day]', N'U') IS NULL
CREATE TABLE [hr].[one day] (
    [id] int NULL
)
GO
-- view and functions
-- non-fk alter table
IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE name = N'PK_staff' AND parent_object_id = OBJECT_ID(N'[hr].[staff]', N'U'))
ALTER TABLE [hr].[staff] ADD CONSTRAINT [PK_staff] PRIMARY KEY CLUSTERED ([id] ASC)
GO
-- table index
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'ix_staff_history' AND object_id = OBJECT_ID(N'[hr].[staff_history]', N'U'))
CREATE CLUSTERED INDEX [ix_staff_history] ON [hr].[staff_history] ([valid_to] ASC, [valid_from] ASC)
GO
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'ix_staff_history_name' AND object_id = OBJECT_ID(N'[hr].[staff_history]', N'U'))
CREATE NONCLUSTERED INDEX [ix_staff_history_name] ON [hr].[staff_history] ([name] ASC)
GO
IF EXISTS (SELECT 1 FROM sys.tables WHERE object_id = OBJECT_ID(N'[hr].[staff]', N'U') AND temporal_type = 0)
ALTER TABLE [hr].[staff] SET (SYSTEM_VERSIONING = ON (HISTORY_TABLE = [hr].[staff_history], HISTORY_RETENTION_PERIOD = 6 MONTHS))
GO
IF EXISTS (SELECT 1 FROM sys.tables WHERE object_id = OBJECT_ID(N'[hr].[kept for ever]', N'U') AND temporal_type = 0)
ALTER TABLE [hr].[kept for ever] SET (SYSTEM_VERSIONING = ON (HISTORY_TABLE = [archive].[kept ]] history], HISTORY_RETENTION_PERIOD = INFINITE))
GO
IF EXISTS (SELECT 1 FROM sys.tables WHERE object_id = OBJECT_ID(N'[hr].[one day]', N'U') AND temporal_type = 0)
ALTER TABLE [hr].[one day] SET (SYSTEM_VERSIONING = ON (HISTORY_TABLE = [hr].[one day history], HISTORY_RETENTION_PERIOD = 1 DAY))
GO
-- fk alter table
-- table triggers
