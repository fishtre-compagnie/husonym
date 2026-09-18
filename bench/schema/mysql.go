package schema

import (
	"fmt"
	"strings"
)

// MySQLRenderer writes MySQL 8 DDL.
type MySQLRenderer struct{}

func (MySQLRenderer) Dialect() Dialect { return MySQL }

func (MySQLRenderer) QuoteIdent(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

func (MySQLRenderer) Placeholder(int) string { return "?" }

func (MySQLRenderer) ExactCollation() string { return "utf8mb4_bin" }

// MySQL prints every value as text over the wire, so a column is read as it is. HEX
// answers in upper case, which the canonical form writes in lower case.
func (r MySQLRenderer) ReadExpr(column string, binary bool) string {
	quoted := r.QuoteIdent(column)
	if binary {
		return "CONCAT('x''', LOWER(HEX(" + quoted + ")), '''')"
	}
	return quoted
}

// MySQL: a case lives in a database of its own.
func (r MySQLRenderer) CreateContainerStatements(container string) []string {
	return []string{
		"DROP DATABASE IF EXISTS " + r.QuoteIdent(container),
		"CREATE DATABASE " + r.QuoteIdent(container),
	}
}

func (r MySQLRenderer) EnsureContainerStatement(container string) string {
	return "CREATE DATABASE IF NOT EXISTS " + r.QuoteIdent(container)
}

func (MySQLRenderer) LoadSessionStatements() []string {
	return []string{"SET FOREIGN_KEY_CHECKS=0"}
}

// MySQL refuses the writes of the whole server, root included with super_read_only.
// Turning read_only off turns super_read_only off with it.
func (MySQLRenderer) ReadOnlyStatement(_ string, readOnly bool) string {
	if readOnly {
		return "SET GLOBAL super_read_only = ON"
	}
	return "SET GLOBAL read_only = OFF"
}

func (MySQLRenderer) Account(user string) string { return "'" + user + "'@'%'" }

// A case is a database of its own: a restricted account has no right on any other, so it
// is the one its connection opens.
func (MySQLRenderer) ConnectionDatabase(_, caseSchema string) string { return caseSchema }

func (r MySQLRenderer) AccountStatements(user, password string) []string {
	return []string{
		"DROP USER IF EXISTS " + r.Account(user),
		"CREATE USER " + r.Account(user) + " IDENTIFIED BY '" + password + "'",
	}
}

func (r MySQLRenderer) CreateTable(database string, t *Table) ([]string, error) {
	lines := make([]string, 0, len(t.Columns)+len(t.Indexes)+1)
	for i := range t.Columns {
		col := &t.Columns[i]
		typ, err := r.columnType(col.Type)
		if err != nil {
			return nil, fmt.Errorf("schema: %s.%s: %w", t.Name, col.Name, err)
		}
		line := r.QuoteIdent(col.Name) + " " + typ
		if collation := col.Collation[MySQL]; collation != "" {
			line += " COLLATE " + collation
		}
		if col.GeneratedAs != "" {
			line += " GENERATED ALWAYS AS (" + col.GeneratedAs + ")"
			if col.GeneratedStored {
				line += " STORED"
			} else {
				line += " VIRTUAL"
			}
		}
		if !col.Nullable {
			line += " NOT NULL"
		}
		if col.Default != "" {
			line += " DEFAULT " + col.Default
		}
		if col.OnUpdate != "" {
			line += " ON UPDATE " + col.OnUpdate
		}
		if col.AutoIncrement {
			line += " AUTO_INCREMENT"
		}
		if col.Invisible {
			line += " INVISIBLE"
		}
		lines = append(lines, line)
	}
	if len(t.PrimaryKey) > 0 {
		lines = append(lines, "PRIMARY KEY ("+r.quoteList(t.PrimaryKey)+")")
	}
	for _, idx := range t.Indexes {
		kind := "KEY"
		if idx.Unique {
			kind = "UNIQUE KEY"
		}
		lines = append(lines, fmt.Sprintf("%s %s (%s)", kind, r.QuoteIdent(idx.Name), r.quoteList(idx.Columns)))
	}
	create := fmt.Sprintf("CREATE TABLE %s.%s (\n  %s\n) ENGINE=InnoDB",
		r.QuoteIdent(database), r.QuoteIdent(t.Name), strings.Join(lines, ",\n  "))
	if t.Options != "" {
		create += " " + t.Options
	}
	return []string{create}, nil
}

func (r MySQLRenderer) AddForeignKeys(database string, t *Table) []string {
	var stmts []string
	for _, fk := range t.ForeignKeys {
		if fk.Virtual {
			continue
		}
		stmt := fmt.Sprintf("ALTER TABLE %s.%s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s.%s (%s)",
			r.QuoteIdent(database), r.QuoteIdent(t.Name), r.QuoteIdent(fk.Name), r.quoteList(fk.Columns),
			r.QuoteIdent(database), r.QuoteIdent(fk.RefTable), r.quoteList(fk.RefColumns))
		if fk.OnDelete != "" {
			stmt += " ON DELETE " + fk.OnDelete
		}
		stmts = append(stmts, stmt)
	}
	return stmts
}

func (r MySQLRenderer) quoteList(names []string) string {
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = r.QuoteIdent(n)
	}
	return strings.Join(quoted, ", ")
}

func (MySQLRenderer) columnType(t Type) (string, error) {
	switch t.Kind {
	case KindInt32:
		return "INT", nil
	case KindInt64:
		return "BIGINT", nil
	case KindUint64:
		return "BIGINT UNSIGNED", nil
	case KindDecimal:
		return fmt.Sprintf("DECIMAL(%d,%d)", t.Precision, t.Scale), nil
	case KindFloat64:
		return "DOUBLE", nil
	case KindBool:
		return "TINYINT(1)", nil
	case KindVarchar:
		return fmt.Sprintf("VARCHAR(%d)", t.Length), nil
	case KindText:
		return "TEXT", nil
	case KindBinary:
		return fmt.Sprintf("BINARY(%d)", t.Length), nil
	case KindBlob:
		return "LONGBLOB", nil
	case KindDate:
		return "DATE", nil
	case KindDateTime:
		return fmt.Sprintf("DATETIME(%d)", t.Length), nil
	case KindJSON:
		return "JSON", nil
	case KindNative:
		if native, ok := t.Native[MySQL]; ok {
			return native, nil
		}
		return "", fmt.Errorf("native type without a MySQL form")
	default:
		return "", fmt.Errorf("unknown kind %d", t.Kind)
	}
}

var _ Renderer = MySQLRenderer{}
