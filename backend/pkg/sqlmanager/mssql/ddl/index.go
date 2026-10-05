package ddl

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// KeyColumns gives the columns of the key of an index, in key order. The columns of an index
// come in index_column_id order, which the key order may differ from.
func (index *Index) KeyColumns() []*IndexColumn {
	keys := []*IndexColumn{}
	for _, column := range index.Columns {
		if column.KeyOrdinal > 0 {
			keys = append(keys, column)
		}
	}
	slices.SortStableFunc(keys, func(a, b *IndexColumn) int { return cmp.Compare(a.KeyOrdinal, b.KeyOrdinal) })
	return keys
}

// keyColumns writes the key of an index: its columns in key order, each with its direction.
func keyColumns(index *Index) string {
	keys := index.KeyColumns()
	written := make([]string, len(keys))
	for i, column := range keys {
		direction := " ASC"
		if column.IsDescending {
			direction = " DESC"
		}
		written[i] = QuoteIdentifier(column.Name) + direction
	}
	return "(" + strings.Join(written, ", ") + ")"
}

// includedColumns names the columns an index carries beside its key. A column that is neither
// a key nor included is one the server added by itself to a partitioned index: it is not one.
func includedColumns(index *Index) []string {
	included := []string{}
	for _, column := range index.Columns {
		if column.IsIncluded {
			included = append(included, column.Name)
		}
	}
	return included
}

// indexOptions writes the options of an index that are not the server's default, or nothing.
func indexOptions(index *Index) string {
	options := []string{}
	if index.IsPadded {
		options = append(options, "PAD_INDEX = ON")
	}
	if index.FillFactor != 0 {
		options = append(options, fmt.Sprintf("FILLFACTOR = %d", index.FillFactor))
	}
	if index.IgnoreDupKey {
		options = append(options, "IGNORE_DUP_KEY = ON")
	}
	if !index.AllowRowLocks {
		options = append(options, "ALLOW_ROW_LOCKS = OFF")
	}
	if !index.AllowPageLocks {
		options = append(options, "ALLOW_PAGE_LOCKS = OFF")
	}
	if len(options) == 0 {
		return ""
	}
	return " WITH (" + strings.Join(options, ", ") + ")"
}

func clustering(index *Index) string {
	if index.Type == IndexClustered || index.Type == IndexClusteredColumnstore {
		return "CLUSTERED"
	}
	return "NONCLUSTERED"
}

// createIndex writes a rowstore or columnstore index that backs no key.
func createIndex(table *Table, index *Index) string {
	on := " ON " + QualifiedName(table.Schema, table.Name)
	name := QuoteIdentifier(index.Name)

	var statement string
	switch index.Type {
	case IndexClusteredColumnstore:
		statement = "CREATE CLUSTERED COLUMNSTORE INDEX " + name + on
	case IndexNonClusteredColumnstore:
		statement = "CREATE NONCLUSTERED COLUMNSTORE INDEX " + name + on +
			" (" + quoteIdentifiers(includedColumns(index)) + ")" + filter(index)
	default:
		unique := ""
		if index.IsUnique {
			unique = "UNIQUE "
		}
		statement = "CREATE " + unique + clustering(index) + " INDEX " + name + on + " " + keyColumns(index)
		if included := includedColumns(index); len(included) > 0 {
			statement += " INCLUDE (" + quoteIdentifiers(included) + ")"
		}
		statement += filter(index) + indexOptions(index)
	}
	return guarded(indexMissing(table.Schema, table.Name, index.Name), statement)
}

func filter(index *Index) string {
	if !index.HasFilter {
		return ""
	}
	return " WHERE " + index.FilterDefinition
}

// disableIndex disables an index for as long as it is enabled.
func disableIndex(table *Table, index *Index) string {
	return guarded(
		indexEnabled(table.Schema, table.Name, index.Name),
		"ALTER INDEX "+QuoteIdentifier(index.Name)+" ON "+QualifiedName(table.Schema, table.Name)+" DISABLE",
	)
}
