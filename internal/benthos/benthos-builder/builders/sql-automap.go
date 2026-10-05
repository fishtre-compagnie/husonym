package benthosbuilder_builders

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
)

// Why AutoMap leaves a column in passthrough, as the run's log words it.
const (
	reasonKey    = "covered by a key"
	reasonCheck  = "under a CHECK constraint"
	reasonNoType = "no transformer for its type"
)

// passedColumns are the new columns AutoMap left in passthrough, as the run's log names
// them: schema.table.column, followed for a column that reads as personal data by its
// category and by why it stays as it is. sensitive counts those.
type passedColumns struct {
	columns   []string
	sensitive int
}

// add records a column left in passthrough. category is empty for a column the detection
// does not read as personal data.
func (p *passedColumns) add(name, category, reason string) {
	if category == "" {
		p.columns = append(p.columns, name)
		return
	}
	p.columns = append(p.columns, fmt.Sprintf("%s (%s, %s)", name, category, reason))
	p.sensitive++
}

func (p *passedColumns) sort() {
	slices.Sort(p.columns)
}

// passedThroughWarning is the line of the run's log about the new columns left as they are.
// It says how many of them read as personal data, and names each of those with its category
// and with why AutoMap did not rewrite it, so that an operator sees what left in clear.
func passedThroughWarning(passed passedColumns) string {
	return fmt.Sprintf(
		"%s passed through as is, awaiting review, %d of them personal data (named with the category and the reason): [%s]",
		unmappedColumns(len(passed.columns)),
		passed.sensitive,
		strings.Join(passed.columns, ", "),
	)
}

// What a CHECK expression holds between single quotes: a text, in which a doubled quote is
// a quote.
var quotedText = regexp.MustCompile(`'(?:[^']|'')*'`)

// checkedColumns are the columns, by schema.table, that a CHECK constraint of their table
// names. The catalogue gives the expression and not its columns: a column is under a
// constraint when its name stands in the expression as an identifier, outside the texts
// the expression holds. A scrambled or generated value would not satisfy the expression,
// and the run would stop on it.
func checkedColumns(
	constraints *sqlmanager_shared.TableConstraints,
	columnInfo map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow,
) map[string]map[string]struct{} {
	out := map[string]map[string]struct{}{}
	if constraints == nil {
		return out
	}
	for table, expressions := range constraints.CheckConstraints {
		for _, expression := range expressions {
			bare := quotedText.ReplaceAllString(expression, "''")
			for column := range columnInfo[table] {
				if !namesColumn(bare, column) {
					continue
				}
				if out[table] == nil {
					out[table] = map[string]struct{}{}
				}
				out[table][column] = struct{}{}
			}
		}
	}
	return out
}

// namesColumn tells whether an expression holds the name of a column as an identifier:
// not as a part of a longer one.
func namesColumn(expression, column string) bool {
	identifier := regexp.MustCompile(`(?i)(?:^|[^A-Za-z0-9_$])` + regexp.QuoteMeta(column) + `(?:$|[^A-Za-z0-9_$])`)
	return identifier.MatchString(expression)
}
