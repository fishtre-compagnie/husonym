IF TYPE_ID(N'[core].[EmailAddress]') IS NULL
CREATE TYPE [core].[EmailAddress] FROM varbinary(50) NULL
GO
IF TYPE_ID(N'[core].[Long]]Text]') IS NULL
CREATE TYPE [core].[Long]]Text] FROM varchar(max) NULL
GO
IF TYPE_ID(N'[core].[it''s money]') IS NULL
CREATE TYPE [core].[it's money] FROM decimal(19,4) NOT NULL
GO
IF TYPE_ID(N'[sales].[EmailAddress]') IS NULL
CREATE TYPE [sales].[EmailAddress] FROM nvarchar(320) NOT NULL
GO
