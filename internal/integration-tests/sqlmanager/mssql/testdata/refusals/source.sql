-- Objects the generator refuses, in the schema refused, and objects it leaves out and reports,
-- in the schema skipped. Batches are separated by lines that hold GO alone.

CREATE SCHEMA refused;
GO
CREATE SCHEMA skipped;
GO
CREATE SCHEMA outside;
GO
CREATE SCHEMA helpers;
GO

-- Tables of another engine.
CREATE TABLE refused.in_memory (
    id int NOT NULL PRIMARY KEY NONCLUSTERED HASH WITH (BUCKET_COUNT = 16),
    v int NULL
) WITH (MEMORY_OPTIMIZED = ON, DURABILITY = SCHEMA_ONLY);
GO
CREATE TABLE refused.graph_node (id int NOT NULL) AS NODE;
GO
CREATE TABLE refused.graph_edge (weight int NULL) AS EDGE;
GO
CREATE TABLE refused.ledger (id int NOT NULL, v int NULL) WITH (LEDGER = ON (APPEND_ONLY = ON));
GO

-- Columns that cannot be written as they are.
CREATE XML SCHEMA COLLECTION refused.shapes AS
N'<xs:schema xmlns:xs="http://www.w3.org/2001/XMLSchema"><xs:element name="shape" type="xs:string"/></xs:schema>';
GO
CREATE TABLE refused.typed_xml (id int NOT NULL, doc xml (refused.shapes) NULL);
GO
SET ANSI_PADDING OFF;
GO
CREATE TABLE refused.not_padded (id int NOT NULL, code varchar(10) NULL, fixed binary(4) NULL, wide nvarchar(10) NULL);
GO
SET ANSI_PADDING ON;
GO
CREATE RULE refused.positive AS @value > 0;
GO
CREATE DEFAULT refused.zero AS 0;
GO
CREATE TABLE refused.bound (id int NOT NULL, ruled int NULL, defaulted int NULL);
GO
EXEC sys.sp_bindrule N'refused.positive', N'refused.bound.ruled';
GO
EXEC sys.sp_bindefault N'refused.zero', N'refused.bound.defaulted';
GO
CREATE TYPE refused.ruled_type FROM int NULL;
GO
EXEC sys.sp_bindrule N'refused.positive', N'refused.ruled_type';
GO
CREATE TABLE refused.bound_type (id int NOT NULL, v refused.ruled_type NULL);
GO

-- A table that cannot be read.
CREATE TABLE refused.disabled_clustered (id int NOT NULL, v int NULL);
GO
CREATE CLUSTERED INDEX CIX_disabled ON refused.disabled_clustered (id);
GO
ALTER INDEX CIX_disabled ON refused.disabled_clustered DISABLE;
GO
CREATE TABLE refused.disabled_key (id int NOT NULL, v int NOT NULL, CONSTRAINT UQ_disabled UNIQUE (v));
GO
ALTER INDEX UQ_disabled ON refused.disabled_key DISABLE;
GO

-- Functions a table calls that cannot be created before it.
CREATE FUNCTION helpers.secret(@n int) RETURNS int WITH ENCRYPTION AS BEGIN RETURN @n END
GO
CREATE TABLE refused.calls_encrypted (id int NOT NULL, v int NULL, CONSTRAINT CK_secret CHECK (helpers.secret(v) > 0));
GO
CREATE TABLE helpers.rates (id int NOT NULL, rate int NOT NULL);
GO
CREATE FUNCTION helpers.bound_rate(@n int) RETURNS int WITH SCHEMABINDING AS
BEGIN
    RETURN (SELECT MAX(rate) FROM helpers.rates WHERE id = @n)
END
GO
CREATE TABLE refused.calls_bound (id int NOT NULL, v AS (helpers.bound_rate(id)));
GO

-- What is left out and reported.
CREATE TABLE outside.parent (id int NOT NULL PRIMARY KEY);
GO
CREATE SYNONYM skipped.parent_synonym FOR outside.parent;
GO
CREATE TYPE skipped.lines AS TABLE (id int NOT NULL);
GO
CREATE SEQUENCE skipped.numbers AS int START WITH 100 INCREMENT BY 1;
GO
CREATE TABLE skipped.child (
    id int NOT NULL CONSTRAINT PK_child PRIMARY KEY CONSTRAINT DF_child_id DEFAULT (NEXT VALUE FOR skipped.numbers),
    parent_id int NULL CONSTRAINT FK_child_outside REFERENCES outside.parent (id),
    doc xml NULL,
    place geometry NULL
);
GO
INSERT INTO skipped.child (parent_id) VALUES (NULL), (NULL);
GO
CREATE PRIMARY XML INDEX XI_child_doc ON skipped.child (doc);
GO
CREATE SPATIAL INDEX SI_child_place ON skipped.child (place) WITH (BOUNDING_BOX = (0, 0, 10, 10));
GO
CREATE VIEW skipped.v_secret WITH ENCRYPTION AS SELECT id FROM skipped.child
GO
CREATE PROCEDURE skipped.p_secret WITH ENCRYPTION AS SELECT 1
GO
CREATE TRIGGER skipped.trg_secret ON skipped.child WITH ENCRYPTION AFTER INSERT AS RETURN
GO
CREATE VIEW skipped.v_outside AS SELECT c.id FROM skipped.child c JOIN outside.parent p ON p.id = c.parent_id
GO
CREATE VIEW skipped.v_on_outside AS SELECT id FROM skipped.v_outside
GO
CREATE PROCEDURE skipped.p_synonym AS SELECT id FROM skipped.parent_synonym
GO
CREATE PROCEDURE skipped.p_table_type @lines skipped.lines READONLY AS SELECT id FROM @lines
GO
CREATE VIEW skipped.v_kept AS SELECT id FROM skipped.child
GO
CREATE TRIGGER skipped.trg_first ON skipped.child AFTER UPDATE AS RETURN
GO
EXEC sys.sp_settriggerorder @triggername = N'skipped.trg_first', @order = N'First', @stmttype = N'UPDATE';
GO
CREATE STATISTICS st_child_parent ON skipped.child (parent_id);
GO

-- Storage and administration: a filegroup of its own, an ordered columnstore index, a security
-- policy, change tracking.
CREATE TABLE skipped.stored (
    id int NOT NULL CONSTRAINT PK_stored PRIMARY KEY NONCLUSTERED ON [PRIMARY],
    tenant int NOT NULL
) ON archive;
GO
CREATE CLUSTERED COLUMNSTORE INDEX CCI_stored ON skipped.stored ORDER (tenant) ON archive;
GO
CREATE FUNCTION helpers.same_tenant(@tenant int) RETURNS TABLE WITH SCHEMABINDING AS
RETURN SELECT 1 AS allowed WHERE @tenant = CAST(SESSION_CONTEXT(N'tenant') AS int)
GO
CREATE SECURITY POLICY helpers.tenants ADD FILTER PREDICATE helpers.same_tenant(tenant) ON skipped.stored;
GO
ALTER TABLE skipped.child ENABLE CHANGE_TRACKING;
GO
