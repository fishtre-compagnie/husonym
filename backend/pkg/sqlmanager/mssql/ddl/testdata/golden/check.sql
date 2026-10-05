IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE name = N'CK it''s' AND parent_object_id = OBJECT_ID(N'[sales].[Order ]] Lines]', N'U'))
ALTER TABLE [sales].[Order ]] Lines] WITH NOCHECK ADD CONSTRAINT [CK it's] CHECK ([it's]<>N'it''s')
GO
IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE name = N'CK_disabled' AND parent_object_id = OBJECT_ID(N'[sales].[Order ]] Lines]', N'U'))
ALTER TABLE [sales].[Order ]] Lines] WITH NOCHECK ADD CONSTRAINT [CK_disabled] CHECK ([qty]<(1000))
GO
IF EXISTS (SELECT 1 FROM sys.check_constraints WHERE name = N'CK_disabled' AND parent_object_id = OBJECT_ID(N'[sales].[Order ]] Lines]', N'U') AND is_disabled = 0)
ALTER TABLE [sales].[Order ]] Lines] NOCHECK CONSTRAINT [CK_disabled]
GO
IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE name = N'CK_lines_qty' AND parent_object_id = OBJECT_ID(N'[sales].[Order ]] Lines]', N'U'))
ALTER TABLE [sales].[Order ]] Lines] WITH CHECK ADD CONSTRAINT [CK_lines_qty] CHECK ([qty]>(0))
GO
IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE name = N'CK_replication' AND parent_object_id = OBJECT_ID(N'[sales].[Order ]] Lines]', N'U'))
ALTER TABLE [sales].[Order ]] Lines] WITH NOCHECK ADD CONSTRAINT [CK_replication] CHECK NOT FOR REPLICATION ([region]>(0))
GO
