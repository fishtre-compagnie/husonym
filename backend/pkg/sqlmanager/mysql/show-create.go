package sqlmanager_mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"

	mysql_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/mysql"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/go-sql-driver/mysql"
	"golang.org/x/sync/errgroup"
)

// The tables of a job are created on the destination from what SHOW CREATE TABLE gives of them
// on the source, and their databases from SHOW CREATE DATABASE. The catalog tables
// (information_schema) tell only part of a table: rebuilding it from them loses its engine, its
// character sets and collations, its row format, its comment, the ON UPDATE clause of its
// columns, its partitioning, the attributes of its keys. The server writes all of it, in the
// syntax of its own version: a 5.7 source gives statements a 5.7 destination takes.
//
// Three things are changed in the statement the server gives:
//   - the table is named with its database, and created IF NOT EXISTS, as the statements built
//     from the catalog were;
//   - its foreign keys are left out: they are added once every table exists, by the statements
//     built from the catalog (see buildAlterStatementByConstraint);
//   - its AUTO_INCREMENT= option, the next value of its counter on the source, is left out: the
//     counter of the destination follows the rows written into it;
//   - an InnoDB table whose row format is not written in it, and differs from the default row
//     format of the source server, has it written (see withImplicitRowFormat).
//
// A table the session cannot read whole (a user granted some of its columns only is refused SHOW
// CREATE TABLE), a view, and a statement this file cannot read, are created from the catalog.

// errShowCreateUnreadable is returned for a statement whose form this file does not know.
var errShowCreateUnreadable = errors.New("unexpected form of SHOW CREATE statement")

// tableDefinition is one definition of the body of a CREATE TABLE statement: a column, a key, a
// constraint.
type tableDefinition struct {
	// text is the definition as the server wrote it, without the whitespace around it.
	text string
	// column is the name of the column a column definition defines.
	column       string
	isColumn     bool
	isForeignKey bool
}

// showCreateTable is a CREATE TABLE statement given by SHOW CREATE TABLE, split into its parts.
type showCreateTable struct {
	definitions []tableDefinition
	// options is what follows the body: the table options, then the partitioning.
	options string
}

// columns gives the names of the columns the statement defines.
func (s *showCreateTable) columns() []string {
	names := []string{}
	for _, def := range s.definitions {
		if def.isColumn {
			names = append(names, def.column)
		}
	}
	return names
}

// createStatement writes the statement that creates the table on the destination: named with its
// database, IF NOT EXISTS, without its foreign keys nor its AUTO_INCREMENT= option.
func (s *showCreateTable) createStatement(schema, table string) string {
	kept := make([]string, 0, len(s.definitions))
	for _, def := range s.definitions {
		if def.isForeignKey {
			continue
		}
		kept = append(kept, "  "+def.text)
	}
	return fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s (\n%s\n)%s;",
		my.Qualified(schema, table),
		strings.Join(kept, ",\n"),
		strings.TrimRight(withoutAutoIncrementOption(s.options), " \n"),
	)
}

// skipQuoted gives the index just past the quoted token that starts at s[i]: an identifier
// between backticks, in which a doubled backtick stands for one, or a string between single or
// double quotes, in which a doubled quote stands for one and a backslash escapes the next byte,
// as SHOW CREATE writes them whatever the sql_mode. The quotes are ASCII and never part of a
// multibyte UTF-8 character: the bytes are scanned as they are.
func skipQuoted(s string, i int) (int, bool) {
	quote := s[i]
	for j := i + 1; j < len(s); j++ {
		switch c := s[j]; {
		case c == '\\' && quote != '`':
			j++
		case c == quote:
			if j+1 < len(s) && s[j+1] == quote {
				j++
				continue
			}
			return j + 1, true
		}
	}
	return len(s), false
}

// readIdentifier reads the identifier between backticks that starts at s[i]: its name, and the
// index just past it.
func readIdentifier(s string, i int) (name string, end int, ok bool) {
	if i >= len(s) || s[i] != '`' {
		return "", i, false
	}
	end, ok = skipQuoted(s, i)
	if !ok {
		return "", i, false
	}
	return strings.ReplaceAll(s[i+1:end-1], "``", "`"), end, true
}

// hasKeyword tells whether s starts with the keyword, in any case, followed by a space, a
// parenthesis or nothing.
func hasKeyword(s, keyword string) bool {
	if len(s) < len(keyword) || !strings.EqualFold(s[:len(keyword)], keyword) {
		return false
	}
	if len(s) == len(keyword) {
		return true
	}
	next := s[len(keyword)]
	return next == ' ' || next == '(' || next == '\n' || next == '\t'
}

// classifyDefinition tells a column definition, by the identifier it starts with, and a foreign
// key, written FOREIGN KEY or CONSTRAINT `name` FOREIGN KEY, from the other definitions.
func classifyDefinition(text string) (tableDefinition, error) {
	def := tableDefinition{text: text}
	if strings.HasPrefix(text, "`") {
		name, _, ok := readIdentifier(text, 0)
		if !ok {
			return def, errShowCreateUnreadable
		}
		def.isColumn, def.column = true, name
		return def, nil
	}
	rest := text
	if hasKeyword(rest, "CONSTRAINT") {
		rest = strings.TrimLeft(rest[len("CONSTRAINT"):], " ")
		if strings.HasPrefix(rest, "`") {
			_, end, ok := readIdentifier(rest, 0)
			if !ok {
				return def, errShowCreateUnreadable
			}
			rest = strings.TrimLeft(rest[end:], " ")
		}
	}
	def.isForeignKey = hasKeyword(rest, "FOREIGN KEY")
	return def, nil
}

// parseShowCreateTable splits what SHOW CREATE TABLE gives of a base table:
//
//	CREATE TABLE `name` (
//	  definition,
//	  definition
//	) options
//
// It refuses any other form: a name quoted otherwise (sql_quote_show_create=OFF, ANSI_QUOTES), a
// view, a sequence.
func parseShowCreateTable(statement string) (*showCreateTable, error) {
	const prefix = "CREATE TABLE "
	if !strings.HasPrefix(statement, prefix) {
		return nil, errShowCreateUnreadable
	}
	_, end, ok := readIdentifier(statement, len(prefix))
	if !ok || !strings.HasPrefix(statement[end:], " (") {
		return nil, errShowCreateUnreadable
	}
	open := end + 1
	parsed := &showCreateTable{}
	depth, start := 0, open+1
	addDefinition := func(text string) error {
		text = strings.TrimSpace(text)
		if text == "" {
			return errShowCreateUnreadable
		}
		def, err := classifyDefinition(text)
		if err != nil {
			return err
		}
		parsed.definitions = append(parsed.definitions, def)
		return nil
	}
	for i := open; i < len(statement); i++ {
		switch statement[i] {
		case '`', '\'', '"':
			next, ok := skipQuoted(statement, i)
			if !ok {
				return nil, errShowCreateUnreadable
			}
			i = next - 1
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				if err := addDefinition(statement[start:i]); err != nil {
					return nil, err
				}
				parsed.options = statement[i+1:]
				return parsed, nil
			}
		case ',':
			if depth == 1 {
				if err := addDefinition(statement[start:i]); err != nil {
					return nil, err
				}
				start = i + 1
			}
		}
	}
	return nil, errShowCreateUnreadable
}

// autoIncrementOption is the AUTO_INCREMENT= table option.
var autoIncrementOption = regexp.MustCompile(`^(?i) AUTO_INCREMENT=\d+`)

// withoutAutoIncrementOption leaves the AUTO_INCREMENT= option out of the table options, and
// nothing else: a string, a comment for instance, that holds the same text is kept.
func withoutAutoIncrementOption(options string) string {
	var out strings.Builder
	for i := 0; i < len(options); {
		switch options[i] {
		case '`', '\'', '"':
			next, _ := skipQuoted(options, i)
			out.WriteString(options[i:next])
			i = next
			continue
		case ' ':
			if match := autoIncrementOption.FindString(options[i:]); match != "" {
				i += len(match)
				continue
			}
		}
		out.WriteByte(options[i])
		i++
	}
	return out.String()
}

// engineOption is the ENGINE= table option, as SHOW CREATE TABLE writes it.
var engineOption = regexp.MustCompile(`^(?i) ENGINE=\w+`)

// withImplicitRowFormat writes in the table options the row format an InnoDB table has on the
// source when SHOW CREATE TABLE does not show it. The server writes ROW_FORMAT= only when it was
// given in the DDL; a table created without it takes innodb_default_row_format, the one of the
// server at the time it was created, and keeps it. A table created under an older default
// (COMPACT, the default before MySQL 5.7.9) reads, on a server where the default is now
// DYNAMIC, as if it had no row format: created from that text on the destination, it would be
// DYNAMIC. rowFormat is information_schema.TABLES.ROW_FORMAT of the source table, defaultFormat
// the innodb_default_row_format of the source server.
//
// The options are left as they are for another engine, a table with a ROW_FORMAT= or a
// KEY_BLOCK_SIZE= (it is COMPRESSED from that one), a row format the server default already
// gives, or a row format that cannot be named in the DDL. The option is written just after
// ENGINE=, where the order of the options does not matter to the server.
func withImplicitRowFormat(options, engine, rowFormat, defaultFormat string) string {
	if !strings.EqualFold(engine, "InnoDB") || defaultFormat == "" ||
		strings.EqualFold(rowFormat, defaultFormat) {
		return options
	}
	format := strings.ToUpper(rowFormat)
	switch format {
	case "COMPACT", "REDUNDANT", "DYNAMIC":
	default:
		return options
	}
	if indexOutsideQuotes(options, " ROW_FORMAT=", 0) >= 0 || indexOutsideQuotes(options, " KEY_BLOCK_SIZE=", 0) >= 0 {
		return options
	}
	for i := 0; i < len(options); {
		switch options[i] {
		case '`', '\'', '"':
			next, ok := skipQuoted(options, i)
			if !ok {
				return options
			}
			i = next
			continue
		case ' ':
			if match := engineOption.FindString(options[i:]); match != "" {
				end := i + len(match)
				return options[:end] + " ROW_FORMAT=" + format + options[end:]
			}
		}
		i++
	}
	return options
}

// innodbDefaultRowFormat reads innodb_default_row_format of the server, in lower case, or ""
// when the server has none (a server that is not InnoDB's, or a session refused the variable):
// the row formats are then written as SHOW CREATE TABLE gives them.
func innodbDefaultRowFormat(ctx context.Context, db queryer) string {
	var format sql.NullString
	if err := db.QueryRowContext(ctx, "SELECT @@GLOBAL.innodb_default_row_format").Scan(&format); err != nil {
		return ""
	}
	return strings.ToLower(format.String)
}

// tableRowFormatOf reads the engine and the row format of a table from information_schema, or
// "" for both when it has no row (a view) or cannot be read.
func tableRowFormatOf(ctx context.Context, db queryer, schema, table string) (engine, rowFormat string) {
	var e, r sql.NullString
	err := db.QueryRowContext(ctx,
		"SELECT ENGINE, ROW_FORMAT FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?",
		schema, table).Scan(&e, &r)
	if err != nil {
		return "", ""
	}
	return e.String, r.String
}

// zeroDateLiteral matches a date literal with a zero year, month or day, such as the
// '0000-00-00 00:00:00' default of a legacy TIMESTAMP column.
var zeroDateLiteral = regexp.MustCompile(`'(?:0000-\d\d-\d\d|\d{4}-00-\d\d|\d{4}-\d\d-00)`)

// hasBackslashInString tells whether a string literal of the statement holds a backslash.
func hasBackslashInString(statement string) bool {
	for i := 0; i < len(statement); i++ {
		switch statement[i] {
		case '`', '\'', '"':
			next, _ := skipQuoted(statement, i)
			if statement[i] != '`' && strings.Contains(statement[i:next], `\`) {
				return true
			}
			i = next - 1
		}
	}
	return false
}

// withDdlSqlMode runs a statement under the sql_mode of the session, without what would make it
// read otherwise than it was written:
//   - NO_ZERO_DATE and NO_ZERO_IN_DATE, with which a strict server (the default of 5.7 and later)
//     refuses a zero date default, such as '0000-00-00 00:00:00', that a source with a laxer mode
//     holds;
//   - NO_BACKSLASH_ESCAPES, with which a backslash that SHOW CREATE writes to escape a character
//     of a string would be read as a backslash.
//
// The mode of the session is set back once the statement has run. A statement that needs
// neither is returned as it is.
func withDdlSqlMode(statement string, escapesWithBackslash bool) string {
	needed := zeroDateLiteral.MatchString(statement) ||
		(escapesWithBackslash && hasBackslashInString(statement))
	if !needed {
		return statement
	}
	return wrapDdlSqlMode(statement)
}

// WithAlterTableSqlMode runs an ALTER TABLE statement of a schema reconciliation under the
// sql_mode withDdlSqlMode sets: on a strict server, an ALTER TABLE that copies a table with a zero
// date default, such as '0000-00-00 00:00:00', is refused whatever column it adds, modifies or
// drops.
func WithAlterTableSqlMode(statement string) string {
	return wrapDdlSqlMode(statement)
}

// wrapDdlSqlMode runs a statement under the sql_mode withDdlSqlMode sets, whatever it holds. An
// ALTER TABLE that copies a table checks the defaults of all its columns again, and a stored
// procedure runs under the sql_mode it was created with: the statements that add a key or a
// constraint to a table with a zero date default are run so too.
func wrapDdlSqlMode(statement string) string {
	return "SET @husonym_ddl_sql_mode = @@SESSION.sql_mode;\n" +
		"SET SESSION sql_mode = REPLACE(REPLACE(REPLACE(@@SESSION.sql_mode, " +
		"'NO_ZERO_IN_DATE', ''), 'NO_ZERO_DATE', ''), 'NO_BACKSLASH_ESCAPES', '');\n" +
		statement + "\n" +
		"SET SESSION sql_mode = @husonym_ddl_sql_mode;"
}

// isAccessDenied tells an error of a server refusing a statement to the session: SHOW CREATE
// TABLE to a user granted some columns of the table only, for instance.
func isAccessDenied(err error) bool {
	var mysqlErr *mysql.MySQLError
	if !errors.As(err, &mysqlErr) {
		return false
	}
	switch mysqlErr.Number {
	case 1044, // ER_DBACCESS_DENIED_ERROR
		1142, // ER_TABLEACCESS_DENIED_ERROR
		1227: // ER_SPECIFIC_ACCESS_DENIED_ERROR
		return true
	}
	return false
}

// showCreateTable reads what SHOW CREATE TABLE gives of a table. ok is false, with no error, for
// a table the session is refused it for, and for a view, which is answered with other columns.
func showCreateTableOf(ctx context.Context, db queryer, schema, table string) (statement string, ok bool, err error) {
	rows, err := db.QueryContext(ctx, "SHOW CREATE TABLE "+my.Qualified(schema, table))
	if err != nil {
		if isAccessDenied(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("unable to read the definition of table %s: %w", my.Qualified(schema, table), err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return "", false, err
	}
	// A view is answered with View, Create View, character_set_client, collation_connection.
	if len(columns) != 2 || !strings.EqualFold(columns[1], "Create Table") {
		return "", false, nil
	}
	if !rows.Next() {
		return "", false, rows.Err()
	}
	var name string
	if err := rows.Scan(&name, &statement); err != nil {
		return "", false, err
	}
	if err := rows.Close(); err != nil {
		return "", false, err
	}
	return statement, true, rows.Err()
}

// showCreateDatabaseOf reads what SHOW CREATE DATABASE IF NOT EXISTS gives of a database, and
// writes the statement that creates it on the destination with its character set and collation:
//
//	CREATE SCHEMA IF NOT EXISTS `name` /*!40100 DEFAULT CHARACTER SET latin1 */
//
// ok is false, with no error, for a database the session is refused it for, or whose statement
// has a form this file does not know.
func showCreateDatabaseOf(ctx context.Context, db queryer, schema string) (statement string, ok bool, err error) {
	var name, shown string
	err = db.QueryRowContext(ctx, "SHOW CREATE DATABASE IF NOT EXISTS "+my.Quote(schema)).Scan(&name, &shown)
	if err != nil {
		if isAccessDenied(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("unable to read the definition of database %s: %w", my.Quote(schema), err)
	}
	statement, err = rewriteShowCreateDatabase(schema, shown)
	if err != nil {
		return "", false, nil //nolint:nilerr // a form not known: the statement is built from the name alone
	}
	return statement, true, nil
}

// rewriteShowCreateDatabase writes, from what SHOW CREATE DATABASE IF NOT EXISTS gives, the
// statement that creates the database: what follows its name is kept as the server wrote it.
func rewriteShowCreateDatabase(schema, shown string) (string, error) {
	const prefix = "CREATE DATABASE /*!32312 IF NOT EXISTS*/ "
	if !strings.HasPrefix(shown, prefix) {
		return "", errShowCreateUnreadable
	}
	_, end, ok := readIdentifier(shown, len(prefix))
	if !ok {
		return "", errShowCreateUnreadable
	}
	rest := strings.TrimRight(shown[end:], " \n;")
	if rest != "" && !strings.HasPrefix(rest, " ") {
		return "", errShowCreateUnreadable
	}
	if err := checkNames("schema", schema); err != nil {
		return "", err
	}
	return withDdlSqlMode(fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s%s;", my.Quote(schema), rest), true), nil
}

// queryer is the part of the connection pool the statements of this file are read with.
type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// showCreateTables reads and splits what SHOW CREATE TABLE gives of each table the catalog
// showed, keyed like the columns read from it. A table missing from the result is created from
// the catalog: the session is refused the statement, the table is a view, or the statement has a
// form this file does not know. A manager without a pool reads nothing.
func (m *MysqlManager) showCreateTables(
	ctx context.Context,
	tables []*sqlmanager_shared.SchemaTable,
	colDefMap map[string][]*mysql_queries.GetDatabaseTableSchemasBySchemasAndTablesRow,
) (map[string]*showCreateTable, error) {
	shown := map[string]*showCreateTable{}
	if m.pool == nil {
		return shown, nil
	}
	defaultFormat := innodbDefaultRowFormat(ctx, m.pool)
	errgrp, errctx := errgroup.WithContext(ctx)
	errgrp.SetLimit(5)
	var mu sync.Mutex
	for _, schematable := range tables {
		key := schematable.String()
		tableData, ok := colDefMap[key]
		if !ok || len(tableData) == 0 {
			continue
		}
		schema, table := tableData[0].SchemaName, tableData[0].TableName
		errgrp.Go(func() error {
			statement, ok, err := showCreateTableOf(errctx, m.pool, schema, table)
			if err != nil || !ok {
				return err
			}
			parsed, err := parseShowCreateTable(statement)
			if err != nil {
				return nil //nolint:nilerr // a form not known: the table is created from the catalog
			}
			if defaultFormat != "" {
				engine, rowFormat := tableRowFormatOf(errctx, m.pool, schema, table)
				parsed.options = withImplicitRowFormat(parsed.options, engine, rowFormat, defaultFormat)
			}
			mu.Lock()
			shown[key] = parsed
			mu.Unlock()
			return nil
		})
	}
	if err := errgrp.Wait(); err != nil {
		return nil, err
	}
	return shown, nil
}

// sameColumns tells whether two lists name the same columns, in any order.
func sameColumns(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

// buildCreateSchemaStatement writes the statement that creates a database with the character set
// and collation it has on the source, or, when SHOW CREATE DATABASE cannot be read, from its name
// alone, with those of the destination server.
func (m *MysqlManager) buildCreateSchemaStatement(ctx context.Context, schema string) (string, error) {
	if err := checkNames("schema", schema); err != nil {
		return "", err
	}
	if m.pool != nil {
		statement, ok, err := showCreateDatabaseOf(ctx, m.pool, schema)
		if err != nil {
			return "", err
		}
		if ok {
			return statement, nil
		}
	}
	return buildCreateSchemaStatement(schema)
}

// matchingParenthesis gives the index of the parenthesis that closes the one at s[open].
func matchingParenthesis(s string, open int) (int, bool) {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '`', '\'', '"':
			next, ok := skipQuoted(s, i)
			if !ok {
				return 0, false
			}
			i = next - 1
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}

// indexOutsideQuotes gives the index of the first occurrence of a keyword sequence, in any case,
// outside quoted tokens, from start.
func indexOutsideQuotes(s, keyword string, start int) int {
	for i := start; i < len(s); i++ {
		switch s[i] {
		case '`', '\'', '"':
			next, ok := skipQuoted(s, i)
			if !ok {
				return -1
			}
			i = next - 1
			continue
		}
		if len(s)-i >= len(keyword) && strings.EqualFold(s[i:i+len(keyword)], keyword) {
			return i
		}
	}
	return -1
}

// useCatalogExpressions writes, in the definitions where SHOW CREATE TABLE replaced characters
// by question marks, the expression the catalog holds. MySQL 8 writes the expression of a CHECK
// constraint and of a generated column through a three-byte character set: a character of four
// bytes (an emoji) comes out as ????, which would change what the constraint checks or the
// column computes. information_schema holds them whole. checks are the clauses of the CHECK
// constraints by name, generated the expressions of the generated columns by column name.
func (s *showCreateTable) useCatalogExpressions(checks, generated map[string]string) {
	for i, def := range s.definitions {
		if !strings.Contains(def.text, "?") {
			continue
		}
		if def.isColumn {
			expression := generated[def.column]
			if expression == "" {
				continue
			}
			_, nameEnd, _ := readIdentifier(def.text, 0)
			at := indexOutsideQuotes(def.text, "GENERATED ALWAYS AS (", nameEnd)
			if at < 0 {
				continue
			}
			open := at + len("GENERATED ALWAYS AS ")
			closing, ok := matchingParenthesis(def.text, open)
			if !ok {
				continue
			}
			s.definitions[i].text = def.text[:open+1] + expression + def.text[closing:]
			continue
		}
		if !hasKeyword(def.text, "CONSTRAINT") {
			continue
		}
		rest := strings.TrimLeft(def.text[len("CONSTRAINT"):], " ")
		name, end, ok := readIdentifier(rest, 0)
		if !ok {
			continue
		}
		clause, found := checks[name]
		if !found || clause == "" {
			continue
		}
		offset := len(def.text) - len(rest) + end
		after := strings.TrimLeft(def.text[offset:], " ")
		if !hasKeyword(after, "CHECK") {
			continue
		}
		open := strings.Index(def.text[offset:], "(")
		if open < 0 {
			continue
		}
		open += offset
		closing, ok := matchingParenthesis(def.text, open)
		if !ok {
			continue
		}
		s.definitions[i].text = def.text[:open] + "(" + clause + ")" + def.text[closing+1:]
	}
}
