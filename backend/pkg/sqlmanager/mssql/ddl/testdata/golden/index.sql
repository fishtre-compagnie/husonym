IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'UX_lines_sku' AND object_id = OBJECT_ID(N'[sales].[Order ]] Lines]', N'U'))
CREATE UNIQUE NONCLUSTERED INDEX [UX_lines_sku] ON [sales].[Order ]] Lines] ([it's] ASC, [id] DESC) INCLUDE ([qty], [note]) WHERE ([qty]>(0))
GO
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'IX options' AND object_id = OBJECT_ID(N'[sales].[Order ]] Lines]', N'U'))
CREATE UNIQUE NONCLUSTERED INDEX [IX options] ON [sales].[Order ]] Lines] ([order id] ASC, [region] ASC) WITH (PAD_INDEX = ON, FILLFACTOR = 70, IGNORE_DUP_KEY = ON, ALLOW_ROW_LOCKS = OFF, ALLOW_PAGE_LOCKS = OFF)
GO
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'IX_disabled' AND object_id = OBJECT_ID(N'[sales].[Order ]] Lines]', N'U'))
CREATE NONCLUSTERED INDEX [IX_disabled] ON [sales].[Order ]] Lines] ([region] ASC)
GO
IF EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'IX_disabled' AND object_id = OBJECT_ID(N'[sales].[Order ]] Lines]', N'U') AND is_disabled = 0)
ALTER INDEX [IX_disabled] ON [sales].[Order ]] Lines] DISABLE
GO
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'IX_partitioned' AND object_id = OBJECT_ID(N'[sales].[Order ]] Lines]', N'U'))
CREATE NONCLUSTERED INDEX [IX_partitioned] ON [sales].[Order ]] Lines] ([order id] ASC)
GO
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'CIX' AND object_id = OBJECT_ID(N'[dbo].[heap]', N'U'))
CREATE CLUSTERED INDEX [CIX] ON [dbo].[heap] ([id] DESC)
GO
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'CCI_facts' AND object_id = OBJECT_ID(N'[dw].[facts]', N'U'))
CREATE CLUSTERED COLUMNSTORE INDEX [CCI_facts] ON [dw].[facts]
GO
IF NOT EXISTS (SELECT 1 FROM sys.indexes WHERE name = N'NCCI_emp' AND object_id = OBJECT_ID(N'[hr].[Employees]', N'U'))
CREATE NONCLUSTERED COLUMNSTORE INDEX [NCCI_emp] ON [hr].[Employees] ([salary], [dept]) WHERE ([dept]>(0))
GO
