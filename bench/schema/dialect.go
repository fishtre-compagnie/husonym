package schema

import "fmt"

// Dialect names a database the bench can create tables in.
type Dialect string

const (
	MySQL Dialect = "mysql"
)

// Dialects lists the databases the bench can create tables in, in report order.
var Dialects = []Dialect{MySQL}

// ParseDialect returns the dialect named s.
func ParseDialect(s string) (Dialect, error) {
	for _, d := range Dialects {
		if string(d) == s {
			return d, nil
		}
	}
	return "", fmt.Errorf("schema: unknown dialect %q", s)
}

// RendererFor returns the renderer writing the DDL of a dialect.
func RendererFor(d Dialect) (Renderer, error) {
	switch d {
	case MySQL:
		return MySQLRenderer{}, nil
	default:
		return nil, fmt.Errorf("schema: no renderer for dialect %q", d)
	}
}
