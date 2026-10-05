IF NOT EXISTS (SELECT 1 FROM sys.schemas WHERE name = N'Sales Ops')
EXEC (N'CREATE SCHEMA [Sales Ops]')
GO
IF NOT EXISTS (SELECT 1 FROM sys.schemas WHERE name = N'a.b')
EXEC (N'CREATE SCHEMA [a.b]')
GO
IF NOT EXISTS (SELECT 1 FROM sys.schemas WHERE name = N'it''s')
EXEC (N'CREATE SCHEMA [it''s]')
GO
IF NOT EXISTS (SELECT 1 FROM sys.schemas WHERE name = N'sales')
EXEC (N'CREATE SCHEMA [sales]')
GO
IF NOT EXISTS (SELECT 1 FROM sys.schemas WHERE name = N'we]ird')
EXEC (N'CREATE SCHEMA [we]]ird]')
GO
