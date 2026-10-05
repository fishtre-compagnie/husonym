IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE name = N'FK_actions' AND parent_object_id = OBJECT_ID(N'[sales].[Order ]] Lines]', N'U'))
ALTER TABLE [sales].[Order ]] Lines] WITH NOCHECK ADD CONSTRAINT [FK_actions] FOREIGN KEY ([order id], [region]) REFERENCES [core].[Orders] ([id], [region]) ON DELETE SET NULL ON UPDATE SET DEFAULT NOT FOR REPLICATION
GO
IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE name = N'FK_lines_order' AND parent_object_id = OBJECT_ID(N'[sales].[Order ]] Lines]', N'U'))
ALTER TABLE [sales].[Order ]] Lines] WITH NOCHECK ADD CONSTRAINT [FK_lines_order] FOREIGN KEY ([order id], [region]) REFERENCES [core].[Orders] ([id], [region]) ON DELETE CASCADE
GO
IF EXISTS (SELECT 1 FROM sys.foreign_keys WHERE name = N'FK_lines_order' AND parent_object_id = OBJECT_ID(N'[sales].[Order ]] Lines]', N'U') AND is_disabled = 0)
ALTER TABLE [sales].[Order ]] Lines] NOCHECK CONSTRAINT [FK_lines_order]
GO
IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE name = N'FK_self' AND parent_object_id = OBJECT_ID(N'[sales].[Order ]] Lines]', N'U'))
ALTER TABLE [sales].[Order ]] Lines] WITH CHECK ADD CONSTRAINT [FK_self] FOREIGN KEY ([order id]) REFERENCES [sales].[Order ]] Lines] ([id])
GO
