IF NOT EXISTS (SELECT 1 FROM sys.triggers WHERE name = N'trg ] off' AND parent_id = OBJECT_ID(N'[sales].[orders]'))
BEGIN
    EXEC (N'SET ANSI_NULLS ON; SET QUOTED_IDENTIFIER ON; EXEC (N''CREATE TRIGGER sales.[trg ]] off] ON sales.orders AFTER DELETE AS RETURN'')');
    IF NOT EXISTS (SELECT 1 FROM sys.triggers WHERE name = N'trg ] off' AND parent_id = OBJECT_ID(N'[sales].[orders]'))
        THROW 50000, N'the definition of trigger [sales].[trg ]] off] did not create it under that name', 1;
END
GO
IF EXISTS (SELECT 1 FROM sys.triggers WHERE name = N'trg ] off' AND parent_id = OBJECT_ID(N'[sales].[orders]') AND is_disabled = 0)
DISABLE TRIGGER [sales].[trg ]] off] ON [sales].[orders]
GO
IF NOT EXISTS (SELECT 1 FROM sys.triggers WHERE name = N'trg_audit' AND parent_id = OBJECT_ID(N'[sales].[orders]'))
BEGIN
    EXEC (N'SET ANSI_NULLS ON; SET QUOTED_IDENTIFIER ON; EXEC (N''CREATE TRIGGER sales.trg_audit ON sales.orders AFTER INSERT AS BEGIN SET NOCOUNT ON; END'')');
    IF NOT EXISTS (SELECT 1 FROM sys.triggers WHERE name = N'trg_audit' AND parent_id = OBJECT_ID(N'[sales].[orders]'))
        THROW 50000, N'the definition of trigger [sales].[trg_audit] did not create it under that name', 1;
END
GO
