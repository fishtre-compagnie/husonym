-- Modules created under each pair of the two session options that a module keeps.

CREATE TABLE dbo.t (id int NOT NULL, v int NULL);
GO
SET QUOTED_IDENTIFIER OFF;
GO
SET ANSI_NULLS OFF;
GO
CREATE VIEW dbo.v_both_off AS SELECT id, "a string" AS label FROM dbo.t WHERE v = NULL
GO
CREATE TRIGGER dbo.trg_both_off ON dbo.t AFTER INSERT AS RETURN
GO
SET ANSI_NULLS ON;
GO
CREATE VIEW dbo.v_quoted_off AS SELECT id, "a string" AS label FROM dbo.t
GO
SET QUOTED_IDENTIFIER ON;
GO
SET ANSI_NULLS OFF;
GO
CREATE VIEW dbo.v_nulls_off AS SELECT id FROM dbo.t WHERE v = NULL
GO
SET ANSI_NULLS ON;
GO
CREATE VIEW dbo.v_both_on AS SELECT id AS "id" FROM dbo.t
GO
