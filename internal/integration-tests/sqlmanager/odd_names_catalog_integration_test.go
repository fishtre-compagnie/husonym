package sqlmanager

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// The catalog is read here by queries of this test alone, with none of the product's: what the
// product created is compared with what the server says it holds.

// queryTexts runs a query and gives its rows, every column as text.
func (d *oddDatabase) queryTexts(t *testing.T, query string) [][]string {
	t.Helper()
	rows, err := d.db.QueryContext(context.Background(), query)
	require.NoError(t, err, query)
	defer rows.Close()
	columns, err := rows.Columns()
	require.NoError(t, err)
	var out [][]string
	for rows.Next() {
		values := make([]sql.NullString, len(columns))
		targets := make([]any, len(columns))
		for i := range values {
			targets[i] = &values[i]
		}
		require.NoError(t, rows.Scan(targets...), query)
		row := make([]string, len(columns))
		for i, value := range values {
			row[i] = value.String
		}
		out = append(out, row)
	}
	require.NoError(t, rows.Err(), query)
	return out
}

const (
	pgUserSchemas    = `n.nspname !~ '^pg_' AND n.nspname <> 'information_schema'`
	mysqlSystemNames = `('mysql', 'information_schema', 'performance_schema', 'sys')`
)

// objectQueries list the schemas, tables, sequences, triggers and routines of a database (of a
// server on MySQL and MariaDB): kind, schema, table for a trigger, name.
var objectQueries = map[string][]string{
	familyPostgres: {
		`SELECT 'schema', n.nspname, '', '' FROM pg_namespace n WHERE ` + pgUserSchemas,
		`SELECT CASE c.relkind WHEN 'S' THEN 'sequence' ELSE 'table' END, n.nspname, '', c.relname
			FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE c.relkind IN ('r', 'p', 'v', 'm', 'f', 'S') AND ` + pgUserSchemas,
		`SELECT 'trigger', n.nspname, c.relname, t.tgname
			FROM pg_trigger t JOIN pg_class c ON c.oid = t.tgrelid JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE NOT t.tgisinternal`,
		`SELECT 'routine', n.nspname, '', p.proname
			FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace WHERE ` + pgUserSchemas,
	},
	familyMysql: {
		`SELECT 'schema', SCHEMA_NAME, '', '' FROM information_schema.SCHEMATA
			WHERE SCHEMA_NAME NOT IN ` + mysqlSystemNames,
		`SELECT 'table', TABLE_SCHEMA, '', TABLE_NAME FROM information_schema.TABLES
			WHERE TABLE_SCHEMA NOT IN ` + mysqlSystemNames,
		`SELECT 'trigger', TRIGGER_SCHEMA, EVENT_OBJECT_TABLE, TRIGGER_NAME FROM information_schema.TRIGGERS
			WHERE TRIGGER_SCHEMA NOT IN ` + mysqlSystemNames,
		`SELECT 'routine', ROUTINE_SCHEMA, '', ROUTINE_NAME FROM information_schema.ROUTINES
			WHERE ROUTINE_SCHEMA NOT IN ` + mysqlSystemNames,
	},
	familyMssql: {
		`SELECT 'schema', name, '', '' FROM sys.schemas
			WHERE schema_id < 16384 AND name NOT IN ('guest', 'INFORMATION_SCHEMA', 'sys')`,
		`SELECT CASE o.type WHEN 'SO' THEN 'sequence' WHEN 'U' THEN 'table' WHEN 'V' THEN 'table' ELSE 'routine' END,
				s.name, '', o.name
			FROM sys.objects o JOIN sys.schemas s ON s.schema_id = o.schema_id
			WHERE o.type IN ('U', 'V', 'SO', 'P', 'FN', 'IF', 'TF') AND o.is_ms_shipped = 0`,
		`SELECT 'trigger', s.name, t.name, tr.name
			FROM sys.triggers tr JOIN sys.tables t ON t.object_id = tr.parent_id
			JOIN sys.schemas s ON s.schema_id = t.schema_id`,
	},
}

func objectLine(kind, schema, table, name string) string {
	return fmt.Sprintf("%s %q %q %q", kind, schema, table, name)
}

// objects lists every schema, table, sequence, trigger and routine of the database, sorted.
func (d *oddDatabase) objects(t *testing.T) []string {
	t.Helper()
	lines := []string{}
	for _, query := range objectQueries[d.f.engine.family] {
		for _, row := range d.queryTexts(t, query) {
			lines = append(lines, objectLine(row[0], row[1], row[2], row[3]))
		}
	}
	sort.Strings(lines)
	return lines
}

// unitObjects lists what a unit is made of, the way objects reads it.
func (d *oddDatabase) unitObjects(u oddUnit) []string {
	lines := []string{
		objectLine("schema", u.schema, "", ""),
		objectLine("table", u.schema, "", u.parent),
		objectLine("table", u.schema, "", u.child),
		objectLine("trigger", u.schema, u.parent, u.trigger),
	}
	switch d.f.engine.family {
	case familyPostgres:
		lines = append(lines,
			objectLine("sequence", u.schema, "", u.counter),
			// The sequence of an identity column is named after its table and its column.
			objectLine("sequence", u.schema, "", u.parent+"_"+u.counter+"_seq"),
			objectLine("routine", u.schema, "", oddTriggerFunction),
		)
	case familyMssql:
		lines = append(lines, objectLine("sequence", u.schema, "", u.counter))
	}
	return lines
}

// requireObjects fails unless the database holds what it held at first and the units, and
// nothing else.
func (d *oddDatabase) requireObjects(t *testing.T, atFirst []string, units []oddUnit) {
	t.Helper()
	want := append([]string{}, atFirst...)
	for _, u := range units {
		want = append(want, d.unitObjects(u)...)
	}
	sort.Strings(want)
	require.Equal(t, want, d.objects(t), "the schemas, tables, sequences, triggers and routines of the database")
}

// descriptionQueries describe the objects of the user schemas: the schema, then one line of text
// per table, column, key, index, constraint, sequence, trigger and routine. The text is built by
// the server, the same way on the source and on the destination. The statement of a MySQL
// trigger is read without the semicolon the product ends it with.
var descriptionQueries = map[string][]string{
	familyPostgres: {
		`SELECT n.nspname, format('table %I kind=%s', c.relname, c.relkind)
			FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE c.relkind IN ('r', 'p') AND ` + pgUserSchemas,
		`SELECT n.nspname, format('column %I %s %I type=%s notnull=%s identity=%s generated=%s default=%s',
				c.relname, a.attnum, a.attname, format_type(a.atttypid, a.atttypmod), a.attnotnull,
				a.attidentity, a.attgenerated, coalesce(pg_get_expr(ad.adbin, ad.adrelid), ''))
			FROM pg_attribute a JOIN pg_class c ON c.oid = a.attrelid JOIN pg_namespace n ON n.oid = c.relnamespace
			LEFT JOIN pg_attrdef ad ON ad.adrelid = a.attrelid AND ad.adnum = a.attnum
			WHERE c.relkind IN ('r', 'p') AND a.attnum > 0 AND NOT a.attisdropped AND ` + pgUserSchemas,
		`SELECT n.nspname, format('constraint %I on %I type=%s: %s',
				con.conname, c.relname, con.contype, pg_get_constraintdef(con.oid))
			FROM pg_constraint con JOIN pg_class c ON c.oid = con.conrelid
			JOIN pg_namespace n ON n.oid = c.relnamespace WHERE ` + pgUserSchemas,
		`SELECT n.nspname, format('index %I on %I: %s', i.indexname, i.tablename, i.indexdef)
			FROM pg_indexes i JOIN pg_namespace n ON n.nspname = i.schemaname WHERE ` + pgUserSchemas,
		`SELECT n.nspname, format('sequence %I type=%s start=%s increment=%s min=%s max=%s cycle=%s',
				s.sequencename, s.data_type, s.start_value, s.increment_by, s.min_value, s.max_value, s.cycle)
			FROM pg_sequences s JOIN pg_namespace n ON n.nspname = s.schemaname WHERE ` + pgUserSchemas,
		`SELECT n.nspname, format('sequence %I belongs to %I.%I (%s)', s.relname, c.relname, a.attname, dep.deptype)
			FROM pg_depend dep
			JOIN pg_class s ON s.oid = dep.objid AND s.relkind = 'S'
			JOIN pg_class c ON c.oid = dep.refobjid
			JOIN pg_attribute a ON a.attrelid = c.oid AND a.attnum = dep.refobjsubid
			JOIN pg_namespace n ON n.oid = s.relnamespace
			WHERE dep.classid = 'pg_class'::regclass AND dep.refclassid = 'pg_class'::regclass
				AND dep.deptype IN ('a', 'i') AND ` + pgUserSchemas,
		`SELECT n.nspname, format('trigger %I on %I: %s', t.tgname, c.relname, pg_get_triggerdef(t.oid))
			FROM pg_trigger t JOIN pg_class c ON c.oid = t.tgrelid JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE NOT t.tgisinternal`,
		`SELECT n.nspname, format('routine %I: %s', p.proname, pg_get_functiondef(p.oid))
			FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
			WHERE p.prokind IN ('f', 'p') AND ` + pgUserSchemas,
	},
	familyMysql: {
		`SELECT TABLE_SCHEMA, CONCAT_WS(' ', 'table', QUOTE(TABLE_NAME), TABLE_TYPE)
			FROM information_schema.TABLES WHERE TABLE_SCHEMA NOT IN ` + mysqlSystemNames,
		`SELECT TABLE_SCHEMA, CONCAT_WS(' ', 'column', QUOTE(TABLE_NAME), ORDINAL_POSITION, QUOTE(COLUMN_NAME),
				COLUMN_TYPE, CONCAT('nullable=', IS_NULLABLE), CONCAT('key=', COLUMN_KEY), CONCAT('extra=', EXTRA),
				CONCAT('default=', IFNULL(COLUMN_DEFAULT, '<none>')))
			FROM information_schema.COLUMNS WHERE TABLE_SCHEMA NOT IN ` + mysqlSystemNames,
		`SELECT TABLE_SCHEMA, CONCAT_WS(' ', 'index', QUOTE(INDEX_NAME), 'on', QUOTE(TABLE_NAME),
				CONCAT('nonunique=', NON_UNIQUE), SEQ_IN_INDEX, QUOTE(COLUMN_NAME))
			FROM information_schema.STATISTICS WHERE TABLE_SCHEMA NOT IN ` + mysqlSystemNames,
		`SELECT TABLE_SCHEMA, CONCAT_WS(' ', 'constraint', QUOTE(CONSTRAINT_NAME), 'on', QUOTE(TABLE_NAME), CONSTRAINT_TYPE)
			FROM information_schema.TABLE_CONSTRAINTS WHERE TABLE_SCHEMA NOT IN ` + mysqlSystemNames,
		`SELECT TABLE_SCHEMA, CONCAT_WS(' ', 'foreign key', QUOTE(CONSTRAINT_NAME), 'on', QUOTE(TABLE_NAME),
				ORDINAL_POSITION, QUOTE(COLUMN_NAME), 'references', QUOTE(REFERENCED_TABLE_SCHEMA),
				QUOTE(REFERENCED_TABLE_NAME), QUOTE(REFERENCED_COLUMN_NAME))
			FROM information_schema.KEY_COLUMN_USAGE
			WHERE REFERENCED_TABLE_NAME IS NOT NULL AND TABLE_SCHEMA NOT IN ` + mysqlSystemNames,
		`SELECT CONSTRAINT_SCHEMA, CONCAT_WS(' ', 'check', QUOTE(CONSTRAINT_NAME), CHECK_CLAUSE)
			FROM information_schema.CHECK_CONSTRAINTS WHERE CONSTRAINT_SCHEMA NOT IN ` + mysqlSystemNames,
		`SELECT TRIGGER_SCHEMA, CONCAT_WS(' ', 'trigger', QUOTE(TRIGGER_NAME), 'on', QUOTE(EVENT_OBJECT_TABLE),
				ACTION_TIMING, EVENT_MANIPULATION, ACTION_ORIENTATION, TRIM(TRAILING ';' FROM ACTION_STATEMENT))
			FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA NOT IN ` + mysqlSystemNames,
		`SELECT ROUTINE_SCHEMA, CONCAT_WS(' ', 'routine', ROUTINE_TYPE, QUOTE(ROUTINE_NAME))
			FROM information_schema.ROUTINES WHERE ROUTINE_SCHEMA NOT IN ` + mysqlSystemNames,
	},
	familyMssql: {
		`SELECT s.name, CONCAT('table ', QUOTENAME(t.name))
			FROM sys.tables t JOIN sys.schemas s ON s.schema_id = t.schema_id`,
		`SELECT s.name, CONCAT('column ', QUOTENAME(t.name), ' ', c.column_id, ' ', QUOTENAME(c.name),
				' type=', ty.name, '(', c.max_length, ',', c.precision, ',', c.scale, ')',
				' nullable=', c.is_nullable, ' identity=', c.is_identity,
				' seed=', CONVERT(nvarchar(40), ic.seed_value), ' increment=', CONVERT(nvarchar(40), ic.increment_value),
				' default=', QUOTENAME(dc.name), ' ', dc.definition)
			FROM sys.columns c JOIN sys.tables t ON t.object_id = c.object_id
			JOIN sys.schemas s ON s.schema_id = t.schema_id
			JOIN sys.types ty ON ty.user_type_id = c.user_type_id
			LEFT JOIN sys.identity_columns ic ON ic.object_id = c.object_id AND ic.column_id = c.column_id
			LEFT JOIN sys.default_constraints dc ON dc.parent_object_id = c.object_id AND dc.parent_column_id = c.column_id`,
		`SELECT s.name, CONCAT('index ', QUOTENAME(i.name), ' on ', QUOTENAME(t.name), ' type=', i.type,
				' unique=', i.is_unique, ' primary=', i.is_primary_key, ' ', ic.key_ordinal, ' ', QUOTENAME(c.name))
			FROM sys.indexes i JOIN sys.tables t ON t.object_id = i.object_id
			JOIN sys.schemas s ON s.schema_id = t.schema_id
			JOIN sys.index_columns ic ON ic.object_id = i.object_id AND ic.index_id = i.index_id
			JOIN sys.columns c ON c.object_id = ic.object_id AND c.column_id = ic.column_id
			WHERE i.name IS NOT NULL`,
		`SELECT s.name, CONCAT('key ', QUOTENAME(kc.name), ' on ', QUOTENAME(t.name), ' type=', kc.type COLLATE DATABASE_DEFAULT)
			FROM sys.key_constraints kc JOIN sys.tables t ON t.object_id = kc.parent_object_id
			JOIN sys.schemas s ON s.schema_id = t.schema_id`,
		`SELECT s.name, CONCAT('foreign key ', QUOTENAME(fk.name), ' on ', QUOTENAME(t.name), ' ',
				fkc.constraint_column_id, ' ', QUOTENAME(pc.name), ' references ',
				QUOTENAME(rs.name), '.', QUOTENAME(rt.name), ' ', QUOTENAME(rc.name))
			FROM sys.foreign_keys fk JOIN sys.tables t ON t.object_id = fk.parent_object_id
			JOIN sys.schemas s ON s.schema_id = t.schema_id
			JOIN sys.foreign_key_columns fkc ON fkc.constraint_object_id = fk.object_id
			JOIN sys.columns pc ON pc.object_id = fkc.parent_object_id AND pc.column_id = fkc.parent_column_id
			JOIN sys.tables rt ON rt.object_id = fkc.referenced_object_id
			JOIN sys.schemas rs ON rs.schema_id = rt.schema_id
			JOIN sys.columns rc ON rc.object_id = fkc.referenced_object_id AND rc.column_id = fkc.referenced_column_id`,
		`SELECT s.name, CONCAT('check ', QUOTENAME(cc.name), ' on ', QUOTENAME(t.name), ' ', cc.definition)
			FROM sys.check_constraints cc JOIN sys.tables t ON t.object_id = cc.parent_object_id
			JOIN sys.schemas s ON s.schema_id = t.schema_id`,
		`SELECT s.name, CONCAT('sequence ', QUOTENAME(sq.name), ' type=', TYPE_NAME(sq.user_type_id),
				' start=', CONVERT(nvarchar(40), sq.start_value), ' increment=', CONVERT(nvarchar(40), sq.increment))
			FROM sys.sequences sq JOIN sys.schemas s ON s.schema_id = sq.schema_id`,
		`SELECT s.name, CONCAT('trigger ', QUOTENAME(tr.name), ' on ', QUOTENAME(t.name), ' ', OBJECT_DEFINITION(tr.object_id))
			FROM sys.triggers tr JOIN sys.tables t ON t.object_id = tr.parent_id
			JOIN sys.schemas s ON s.schema_id = t.schema_id`,
	},
}

// describe gives one line per object of the given schemas, sorted, each led by its schema.
func (d *oddDatabase) describe(t *testing.T, schemas []string) []string {
	t.Helper()
	wanted := map[string]bool{}
	for _, schema := range schemas {
		wanted[schema] = true
	}
	lines := []string{}
	for _, query := range descriptionQueries[d.f.engine.family] {
		for _, row := range d.queryTexts(t, query) {
			if wanted[row[0]] {
				lines = append(lines, fmt.Sprintf("%q: %s", row[0], strings.TrimSpace(row[1])))
			}
		}
	}
	sort.Strings(lines)
	return lines
}
