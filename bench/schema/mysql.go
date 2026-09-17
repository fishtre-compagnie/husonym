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

func (r MySQLRenderer) CreateTable(database string, t *Table) (string, error) {
	lines := make([]string, 0, len(t.Columns)+len(t.Indexes)+1)
	for i := range t.Columns {
		col := &t.Columns[i]
		typ, err := r.columnType(col.Type)
		if err != nil {
			return "", fmt.Errorf("schema: %s.%s: %w", t.Name, col.Name, err)
		}
		line := r.QuoteIdent(col.Name) + " " + typ
		if col.Collation != "" {
			line += " COLLATE " + col.Collation
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
	return create, nil
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
