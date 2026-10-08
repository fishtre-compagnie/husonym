-- A start of the service under a role named like the schema used to look for the bookkeeping
-- table of the migrations in the schema controlplane, create a second one there and leave it
-- dirty. The bookkeeping is in public; a database that went through that start loses the stray
-- table here, and any other database loses nothing.
DROP TABLE IF EXISTS controlplane.schema_migrations;
