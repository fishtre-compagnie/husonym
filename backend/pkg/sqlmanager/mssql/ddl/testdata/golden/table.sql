IF OBJECT_ID(N'[sales].[Order ]] Lines]', N'U') IS NULL
CREATE TABLE [sales].[Order ]] Lines] (
    [id] int IDENTITY(1,1) NOT NULL,
    [it's] nvarchar(40) COLLATE Latin1_General_100_CI_AS NOT NULL,
    [note] varchar(max) COLLATE Latin1_General_100_CI_AS NULL,
    [email] [sales].[EmailAddress] NOT NULL,
    [qty] decimal(9,2) CONSTRAINT [DF_lines_qty] DEFAULT ((1)) NOT NULL,
    [total] AS ([qty]*(2)) PERSISTED NOT NULL,
    [order id] int NOT NULL,
    [region] int NOT NULL
)
GO
IF OBJECT_ID(N'[dbo].[every column]', N'U') IS NULL
CREATE TABLE [dbo].[every column] (
    [big id] decimal(20,0) IDENTITY(-5,10) NOT FOR REPLICATION NOT NULL,
    [guid] uniqueidentifier CONSTRAINT [DF__every__guid__5EBF139D] DEFAULT (newid()) NOT NULL ROWGUIDCOL,
    [sparse] int SPARSE NULL,
    [set] xml COLUMN_SET FOR ALL_SPARSE_COLUMNS NULL,
    [masked] varchar(20) COLLATE Latin1_General_100_CI_AS MASKED WITH (FUNCTION = N'partial(1, "x''x", 0)') NULL,
    [it's] nchar(10) COLLATE Latin1_General_100_CI_AS CONSTRAINT [DF it's] DEFAULT (N'it''s') NOT NULL,
    [plain] AS ([sparse]+(1)),
    [stored] AS (isnull([sparse],(0))) PERSISTED NOT NULL,
    [stored nullable] AS ([sparse]*(2)) PERSISTED,
    [version] timestamp NOT NULL,
    [when] datetime2(3) NULL,
    [shape] geography NULL,
    [label] sysname COLLATE Latin1_General_100_CI_AS NOT NULL,
    [amount] [dbo].[Money]]4] NULL
)
GO
IF OBJECT_ID(N'[hr].[period only]', N'U') IS NULL
CREATE TABLE [hr].[period only] (
    [id] int NOT NULL,
    [valid from] datetime2(7) GENERATED ALWAYS AS ROW START NOT NULL,
    [valid to] datetime2(7) GENERATED ALWAYS AS ROW END HIDDEN NOT NULL,
    PERIOD FOR SYSTEM_TIME ([valid from], [valid to])
)
GO
