IF OBJECT_ID(N'[sales].[trg ]] off]', N'TR') IS NULL
BEGIN
    EXEC (N'SET ANSI_NULLS ON; SET QUOTED_IDENTIFIER ON; EXEC (N''CREATE TRIGGER sales.[trg ]] off] ON sales.orders AFTER DELETE AS RETURN'')');
    IF OBJECT_ID(N'[sales].[trg ]] off]', N'TR') IS NULL
        THROW 50000, N'the definition of trigger [sales].[trg ]] off] did not create it under that name', 1;
END
GO
IF EXISTS (SELECT 1 FROM sys.triggers WHERE object_id = OBJECT_ID(N'[sales].[trg ]] off]', N'TR') AND is_disabled = 0)
DISABLE TRIGGER [sales].[trg ]] off] ON [sales].[orders]
GO
IF OBJECT_ID(N'[sales].[trg_audit]', N'TR') IS NULL
BEGIN
    EXEC (N'SET ANSI_NULLS ON; SET QUOTED_IDENTIFIER ON; EXEC (N''CREATE TRIGGER sales.trg_audit ON sales.orders AFTER INSERT AS BEGIN SET NOCOUNT ON; END'')');
    IF OBJECT_ID(N'[sales].[trg_audit]', N'TR') IS NULL
        THROW 50000, N'the definition of trigger [sales].[trg_audit] did not create it under that name', 1;
END
GO
