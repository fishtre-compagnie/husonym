package sqlmanager_mssql

import (
	"cmp"
	"context"
	"slices"

	mssql_queries "github.com/fishtre-compagnie/husonym/backend/pkg/mssql-querier"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/mssql/ddl"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"golang.org/x/sync/errgroup"
)

// GetTableConstraintsBySchema gives, for the tables of the given schemas, their foreign keys,
// primary keys, unique constraints and unique indexes, by table. Columns come in key order.
func (m *Manager) GetTableConstraintsBySchema(
	ctx context.Context,
	schemas []string,
) (*sqlmanager_shared.TableConstraints, error) {
	if len(schemas) == 0 {
		return &sqlmanager_shared.TableConstraints{}, nil
	}

	var (
		indexes     []*mssql_queries.GetIndexesRow
		foreignKeys []*mssql_queries.GetForeignKeysRow
	)
	errgrp, errctx := errgroup.WithContext(ctx)
	errgrp.Go(func() (err error) {
		indexes, err = m.querier.GetIndexesBySchemas(errctx, m.db, schemas)
		return err
	})
	errgrp.Go(func() (err error) {
		foreignKeys, err = m.querier.GetForeignKeysBySchemas(errctx, m.db, schemas)
		return err
	})
	if err := errgrp.Wait(); err != nil {
		return nil, err
	}

	// The rows name their table: the key of the results is schema.table.
	tables := map[int64]string{}
	for _, row := range indexes {
		tables[row.ObjectID] = sqlmanager_shared.BuildTable(row.TableSchema, row.TableName)
	}
	for _, row := range foreignKeys {
		tables[row.ObjectID] = sqlmanager_shared.BuildTable(row.TableSchema, row.TableName)
	}

	constraints := &sqlmanager_shared.TableConstraints{
		ForeignKeyConstraints: map[string][]*sqlmanager_shared.ForeignConstraint{},
		PrimaryKeyConstraints: map[string][]string{},
		UniqueConstraints:     map[string][][]string{},
		UniqueIndexes:         map[string][][]string{},
	}
	for id, list := range groupIndexes(indexes) {
		table := tables[id]
		// Unique constraints come by name, unique indexes in index_id order.
		byName := slices.Clone(list)
		slices.SortStableFunc(byName, func(a, b *ddl.Index) int { return cmp.Compare(a.Name, b.Name) })
		for _, index := range byName {
			switch {
			case index.IsPrimaryKey:
				constraints.PrimaryKeyConstraints[table] = keyColumnNames(index)
			case index.IsUniqueConstraint:
				constraints.UniqueConstraints[table] = append(constraints.UniqueConstraints[table], keyColumnNames(index))
			}
		}
		for _, index := range list {
			if index.IsUnique && !index.IsPrimaryKey && !index.IsUniqueConstraint {
				constraints.UniqueIndexes[table] = append(constraints.UniqueIndexes[table], keyColumnNames(index))
			}
		}
	}
	for id, list := range groupForeignKeys(foreignKeys) {
		table := tables[id]
		for _, key := range list {
			referenced := sqlmanager_shared.BuildTable(key.ReferencedSchema, key.ReferencedTable)
			constraint := &sqlmanager_shared.ForeignConstraint{
				Columns:     make([]string, len(key.Columns)),
				NotNullable: make([]bool, len(key.Columns)),
				ForeignKey:  &sqlmanager_shared.ForeignKey{Table: referenced, Columns: make([]string, len(key.Columns))},
			}
			for i, column := range key.Columns {
				constraint.Columns[i] = column.Name
				constraint.NotNullable[i] = !column.IsNullable
				constraint.ForeignKey.Columns[i] = column.ReferencedName
			}
			if id == key.ReferencedID && referencesItsOwnColumns(constraint.Columns, constraint.ForeignKey.Columns) {
				continue
			}
			constraints.ForeignKeyConstraints[table] = append(constraints.ForeignKeyConstraints[table], constraint)
		}
	}
	return constraints, nil
}

func keyColumnNames(index *ddl.Index) []string {
	keys := index.KeyColumns()
	names := make([]string, len(keys))
	for i, column := range keys {
		names[i] = column.Name
	}
	return names
}

// referencesItsOwnColumns tells a foreign key of a table to itself whose every column is among
// the columns it references, as a column that references itself is: it orders nothing.
func referencesItsOwnColumns(columns, referencedColumns []string) bool {
	for _, column := range columns {
		if !slices.Contains(referencedColumns, column) {
			return false
		}
	}
	return true
}
