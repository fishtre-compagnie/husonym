IF OBJECT_ID(N'[sales].[fn_inline]', N'IF') IS NULL
BEGIN
    EXEC (N'SET ANSI_NULLS OFF; SET QUOTED_IDENTIFIER ON; EXEC (N''CREATE FUNCTION sales.fn_inline(@id int) RETURNS TABLE AS RETURN (SELECT @id AS id)'')');
    IF OBJECT_ID(N'[sales].[fn_inline]', N'IF') IS NULL
        THROW 50000, N'the definition of function [sales].[fn_inline] did not create it under that name', 1;
END
GO
IF OBJECT_ID(N'[sales].[it''s a proc]', N'P') IS NULL
BEGIN
    EXEC (N'SET ANSI_NULLS ON; SET QUOTED_IDENTIFIER ON; EXEC (N''CREATE PROCEDURE sales.[it''''s a proc] AS BEGIN SET NOCOUNT ON; SELECT 1; END'')');
    IF OBJECT_ID(N'[sales].[it''s a proc]', N'P') IS NULL
        THROW 50000, N'the definition of procedure [sales].[it''s a proc] did not create it under that name', 1;
END
GO
IF OBJECT_ID(N'[sales].[v "quoted" 100%]', N'V') IS NULL
BEGIN
    EXEC (N'SET ANSI_NULLS ON; SET QUOTED_IDENTIFIER OFF; EXEC (N''create view sales.[v "quoted" 100%] as
select "it''''s" as [a]]b], ''''x''''''''y'''' as c'')');
    IF OBJECT_ID(N'[sales].[v "quoted" 100%]', N'V') IS NULL
        THROW 50000, N'the definition of view [sales].[v "quoted" 100%%] did not create it under that name', 1;
END
GO
IF OBJECT_ID(N'[sales].[v_open]', N'V') IS NULL
BEGIN
    EXEC (N'SET ANSI_NULLS ON; SET QUOTED_IDENTIFIER ON; EXEC (N''CREATE VIEW [sales].[v_open] AS SELECT id FROM sales.orders WHERE state = ''''open'''''')');
    IF OBJECT_ID(N'[sales].[v_open]', N'V') IS NULL
        THROW 50000, N'the definition of view [sales].[v_open] did not create it under that name', 1;
END
GO
