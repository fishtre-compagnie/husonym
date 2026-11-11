package sqlmanager_mysql

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Triggers and functions are created on the destination from what SHOW CREATE TRIGGER and SHOW
// CREATE FUNCTION give of them on the source, under the sql_mode and the character sets they
// were created with, as mysqldump does. Rebuilt from information_schema, a function loses its
// characteristics (NO SQL, READS SQL DATA, SQL SECURITY, COMMENT), the way its parameters are
// written, and the character set of its return type; a trigger loses the text it was created
// with, and every one of them takes the sql_mode and collation of the session that recreates it,
// which change what it does.
//
// The DEFINER clause is left out: the object belongs to the account that creates it, which
// needs no privilege to name another one. A trigger is created from its original text, which
// names its table without its database: it is run in the database of the table (USE), which
// the session keeps afterwards. A function is named with its database.

// shownRoutine is what SHOW CREATE TRIGGER or SHOW CREATE FUNCTION gives of an object.
type shownRoutine struct {
	statement          string
	sqlMode            string
	characterSetClient string
	collation          string
}

// showCreateRoutineOf reads SHOW CREATE <kind> of an object, kind being TRIGGER or FUNCTION. ok
// is false, with no error, when the session is refused the statement or is not shown it (SHOW
// CREATE FUNCTION gives a NULL statement to an account that may not read the body).
func showCreateRoutineOf(
	ctx context.Context,
	db queryer,
	kind, schema, name string,
) (routine *shownRoutine, ok bool, err error) {
	rows, err := db.QueryContext(ctx, fmt.Sprintf("SHOW CREATE %s %s", kind, my.Qualified(schema, name)))
	if err != nil {
		if isAccessDenied(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("unable to read the definition of %s %s: %w",
			strings.ToLower(kind), my.Qualified(schema, name), err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, false, err
	}
	if !rows.Next() {
		return nil, false, rows.Err()
	}
	values := make([]sql.NullString, len(columns))
	targets := make([]any, len(columns))
	for i := range values {
		targets[i] = &values[i]
	}
	if err := rows.Scan(targets...); err != nil {
		return nil, false, err
	}
	routine = &shownRoutine{}
	var statement sql.NullString
	for i, column := range columns {
		switch strings.ToLower(column) {
		case "sql original statement", "create function":
			statement = values[i]
		case "sql_mode":
			routine.sqlMode = values[i].String
		case "character_set_client":
			routine.characterSetClient = values[i].String
		case "collation_connection":
			routine.collation = values[i].String
		}
	}
	if err := rows.Close(); err != nil {
		return nil, false, err
	}
	if !statement.Valid || statement.String == "" || !charsetName.MatchString(routine.collation) {
		return nil, false, nil
	}
	routine.statement = statement.String
	return routine, true, nil
}

// skipAccountPart gives the index past one part of an account, user or host: quoted, or a bare
// word ending at the @ or at a space.
func skipAccountPart(s string, i int, stops string) (int, bool) {
	if i >= len(s) {
		return i, false
	}
	switch s[i] {
	case '`', '\'', '"':
		return skipQuoted(s, i)
	}
	j := i
	for j < len(s) && !strings.ContainsRune(stops, rune(s[j])) {
		j++
	}
	return j, j > i
}

// withoutDefiner takes the DEFINER clause out of a CREATE statement of a trigger or a routine,
// and checks that the object kind follows: CREATE DEFINER=`u`@`h` TRIGGER ... becomes CREATE
// TRIGGER .... It gives what follows CREATE, without the clause.
func withoutDefiner(statement, kind string) (string, bool) {
	const create = "CREATE "
	if !strings.HasPrefix(statement, create) {
		return "", false
	}
	rest := statement[len(create):]
	if len(rest) >= len("DEFINER") && strings.EqualFold(rest[:len("DEFINER")], "DEFINER") {
		i := strings.IndexFunc(rest[len("DEFINER"):], func(r rune) bool { return r != ' ' })
		if i < 0 || rest[len("DEFINER")+i] != '=' {
			return "", false
		}
		i = len("DEFINER") + i + 1
		for i < len(rest) && rest[i] == ' ' {
			i++
		}
		if strings.HasPrefix(strings.ToUpper(rest[i:]), "CURRENT_USER") {
			i += len("CURRENT_USER")
			if strings.HasPrefix(rest[i:], "()") {
				i += 2
			}
		} else {
			end, ok := skipAccountPart(rest, i, "@ ")
			if !ok || end >= len(rest) || rest[end] != '@' {
				return "", false
			}
			end, ok = skipAccountPart(rest, end+1, " ")
			if !ok {
				return "", false
			}
			i = end
		}
		rest = strings.TrimLeft(rest[i:], " ")
	}
	if !hasKeyword(rest, kind) {
		return "", false
	}
	return rest, true
}

// utf8Charsets are the character sets the text of a statement is sent in by this package's
// connections, or that read it the same: a statement is sent as UTF-8.
var utf8Charsets = map[string]bool{"utf8": true, "utf8mb3": true, "utf8mb4": true}

// withRoutineSession runs the statements that create a trigger or a routine under the sql_mode,
// the client character set and the collation it was created with, then gives the session its
// own back. The client character set is set only when it reads the UTF-8 the statement is sent
// in: the object then takes the one of the session.
func withRoutineSession(routine *shownRoutine, statements string) string {
	settings := "sql_mode = " + my.Literal(routine.sqlMode)
	if utf8Charsets[strings.ToLower(routine.characterSetClient)] {
		settings += ", character_set_client = " + routine.characterSetClient
	}
	settings += ", collation_connection = " + routine.collation
	return "SET @husonym_routine_sql_mode = @@SESSION.sql_mode, " +
		"@husonym_routine_character_set_client = @@SESSION.character_set_client, " +
		"@husonym_routine_collation_connection = @@SESSION.collation_connection;\n" +
		"SET SESSION " + settings + ";\n" +
		statements + "\n" +
		"SET SESSION sql_mode = @husonym_routine_sql_mode, " +
		"character_set_client = @husonym_routine_character_set_client, " +
		"collation_connection = @husonym_routine_collation_connection;"
}

// buildTriggerFromShowCreate writes the statements that create a trigger from its original
// text: in the database of its table, after dropping one of the same name, which a CREATE
// TRIGGER IF NOT EXISTS would have written into the text.
func buildTriggerFromShowCreate(tableSchema, triggerSchema, triggerName string, routine *shownRoutine) (string, bool) {
	created, ok := withoutDefiner(routine.statement, "TRIGGER")
	if !ok {
		return "", false
	}
	return withRoutineSession(routine,
		"USE "+my.Quote(tableSchema)+";\n"+
			BuildDropTriggerStatement(&triggerSchema, triggerName)+"\n"+
			"CREATE "+created+";",
	), true
}

// buildFunctionFromShowCreate writes the statements that create a function from what SHOW
// CREATE FUNCTION gives, named with its database, IF NOT EXISTS where the server takes it, after
// dropping it where it does not.
func buildFunctionFromShowCreate(flavor serverFlavor, schema, name string, routine *shownRoutine) (string, bool) {
	created, ok := withoutDefiner(routine.statement, "FUNCTION")
	if !ok {
		return "", false
	}
	rest := strings.TrimLeft(created[len("FUNCTION"):], " ")
	if !strings.HasPrefix(rest, "`") {
		return "", false
	}
	_, end, ok := readIdentifier(rest, 0)
	if !ok {
		return "", false
	}
	if !flavor.createsRoutinesIfNotExists() {
		return withRoutineSession(routine,
			BuildDropFunctionStatement(schema, name)+"\n"+
				"CREATE FUNCTION "+my.Qualified(schema, name)+rest[end:]+";",
		), true
	}
	return withRoutineSession(routine,
		"CREATE FUNCTION IF NOT EXISTS "+my.Qualified(schema, name)+rest[end:]+";",
	), true
}

// showCreateTrigger reads SHOW CREATE TRIGGER of a trigger; nil when it cannot be read, or by a
// manager without a pool.
func (m *MysqlManager) showCreateTrigger(ctx context.Context, schema, name string) (*shownRoutine, error) {
	if m.pool == nil {
		return nil, nil
	}
	routine, ok, err := showCreateRoutineOf(ctx, m.pool, "TRIGGER", schema, name)
	if err != nil || !ok {
		return nil, err
	}
	return routine, nil
}

// showCreateFunction reads SHOW CREATE FUNCTION of a function; nil when it cannot be read, or by
// a manager without a pool.
func (m *MysqlManager) showCreateFunction(ctx context.Context, schema, name string) (*shownRoutine, error) {
	if m.pool == nil {
		return nil, nil
	}
	routine, ok, err := showCreateRoutineOf(ctx, m.pool, "FUNCTION", schema, name)
	if err != nil || !ok {
		return nil, err
	}
	return routine, nil
}
