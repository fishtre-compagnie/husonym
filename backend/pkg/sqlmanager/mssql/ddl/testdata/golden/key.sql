IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE name = N'PK_lines' AND parent_object_id = OBJECT_ID(N'[sales].[Order ]] Lines]', N'U'))
ALTER TABLE [sales].[Order ]] Lines] ADD CONSTRAINT [PK_lines] PRIMARY KEY CLUSTERED ([id] ASC) WITH (FILLFACTOR = 90)
GO
IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE name = N'UQ_a' AND parent_object_id = OBJECT_ID(N'[sales].[Order ]] Lines]', N'U'))
ALTER TABLE [sales].[Order ]] Lines] ADD CONSTRAINT [UQ_a] UNIQUE NONCLUSTERED ([it's] ASC)
GO
IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE name = N'UQ_b' AND parent_object_id = OBJECT_ID(N'[sales].[Order ]] Lines]', N'U'))
ALTER TABLE [sales].[Order ]] Lines] ADD CONSTRAINT [UQ_b] UNIQUE NONCLUSTERED ([region] DESC, [order id] ASC) WITH (PAD_INDEX = ON, FILLFACTOR = 80, IGNORE_DUP_KEY = ON, ALLOW_ROW_LOCKS = OFF, ALLOW_PAGE_LOCKS = OFF)
GO
IF NOT EXISTS (SELECT 1 FROM sys.objects WHERE name = N'PK__heap__3213E83F' AND parent_object_id = OBJECT_ID(N'[dbo].[heap]', N'U'))
ALTER TABLE [dbo].[heap] ADD CONSTRAINT [PK__heap__3213E83F] PRIMARY KEY NONCLUSTERED ([id] ASC)
GO
