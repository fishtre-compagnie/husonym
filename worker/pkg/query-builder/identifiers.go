package querybuilder

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/doug-martin/goqu/v9"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared/sqlident"
)

// tableDialect gives the dialect of a driver for a statement on a table. It refuses a
// driver without a dialect, and a schema or a table name that no engine takes. A table
// may have no schema: it is then written alone.
func tableDialect(driver, schema, table string) (sqlident.Dialect, error) {
	d, err := sqlident.ForDriver(driver)
	if err != nil {
		return 0, err
	}
	if err := checkTable(schema, table); err != nil {
		return 0, err
	}
	return d, nil
}

func checkTable(schema, table string) error {
	if schema != "" {
		if err := sqlident.Check(schema); err != nil {
			return fmt.Errorf("schema name: %w", err)
		}
	}
	if err := sqlident.Check(table); err != nil {
		return fmt.Errorf("table name: %w", err)
	}
	return nil
}

func checkColumns(columns []string) error {
	for _, column := range columns {
		if err := sqlident.Check(column); err != nil {
			return fmt.Errorf("column name: %w", err)
		}
	}
	return nil
}

// insertInto starts an INSERT of the records into a table, prepared. The columns are
// those of the first record, in name order, and every record must hold the same ones.
// Records without a column are handed to goqu as they are: they name nothing.
func insertInto(driver, schema, table string, records []goqu.Record) (*goqu.InsertDataset, sqlident.Dialect, error) {
	d, err := tableDialect(driver, schema, table)
	if err != nil {
		return nil, 0, err
	}
	insert := getGoquDialect(driver).Insert(d.Table(schema, table)).Prepared(true)
	if len(records) == 0 || len(records[0]) == 0 {
		return insert.Rows(records), d, nil
	}

	columns := sortedColumns(records[0])
	if err := checkColumns(columns); err != nil {
		return nil, 0, err
	}
	cols := make([]any, len(columns))
	for i, column := range columns {
		cols[i] = d.Col(column)
	}
	vals := make([][]any, len(records))
	for i, record := range records {
		if len(record) != len(columns) {
			return nil, 0, fmt.Errorf("rows with different value length expected %d got %d", len(columns), len(record))
		}
		vals[i] = make([]any, len(columns))
		for j, column := range columns {
			val, ok := record[column]
			if !ok {
				return nil, 0, errors.New("rows with different columns")
			}
			vals[i][j] = val
		}
	}
	return insert.Cols(cols...).Vals(vals...), d, nil
}

// sortedColumns gives the columns of a record in name order, so the same rows always
// build the same query.
func sortedColumns(record goqu.Record) []string {
	columns := make([]string, 0, len(record))
	for column := range record {
		columns = append(columns, column)
	}
	slices.Sort(columns)
	return columns
}

// setColumns gives the assignments of an UPDATE or of a conflict clause: each column, in
// name order, set to value(column).
//
// goqu takes several columns to set as the string keys of a record only, and reads a dot
// in a key as a separator and the key * as the star. The assignments are therefore given
// as one expression: the first column, set to a literal that carries the other columns as
// identifiers and every value as an argument. goqu writes it as it writes a record:
// "a"=1,"b"=2.
//
// Without a column there is nothing to name: the empty record is returned, which goqu
// refuses.
func setColumns(d sqlident.Dialect, columns []string, value func(column string) any) any {
	columns = slices.Clone(columns)
	slices.Sort(columns)
	columns = slices.Compact(columns)
	if len(columns) == 0 {
		return goqu.Record{}
	}
	first, others := columns[0], columns[1:]
	if len(others) == 0 {
		return d.Col(first).Set(value(first))
	}
	var list strings.Builder
	list.WriteString("?")
	args := make([]any, 0, 1+2*len(others))
	args = append(args, value(first))
	for _, column := range others {
		list.WriteString(",?=?")
		args = append(args, d.Col(column), value(column))
	}
	return d.Col(first).Set(goqu.L(list.String(), args...))
}
