package sqlmanager_mssql

import (
	"context"
	"fmt"

	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/mssql/ddl"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
)

// plan reads the catalog for the tables and turns it into the statements that create them. A
// selection the generator refuses comes back as a *ddl.RefusalError.
func (m *Manager) plan(ctx context.Context, tables []*sqlmanager_shared.SchemaTable) (*ddl.Plan, error) {
	if len(tables) == 0 {
		return ddl.Build(&ddl.Snapshot{})
	}
	snapshot, err := m.snapshot(ctx, tables)
	if err != nil {
		return nil, fmt.Errorf("unable to read the sql server catalog: %w", err)
	}
	plan, err := ddl.Build(snapshot)
	if err != nil {
		return nil, fmt.Errorf("unable to reproduce the sql server schema:\n%w", err)
	}
	return plan, nil
}

// GetSchemaInitStatements gives the eight blocks that create the tables and what they bring with
// them, in execution order. Each block tells what it leaves out.
func (m *Manager) GetSchemaInitStatements(
	ctx context.Context,
	tables []*sqlmanager_shared.SchemaTable,
) ([]*sqlmanager_shared.InitSchemaStatements, error) {
	plan, err := m.plan(ctx, tables)
	if err != nil {
		return nil, err
	}
	return plan.Blocks(), nil
}

// GetTableInitStatements gives, per table found, the statement that creates it, those that add
// its constraints, each with its kind, and those that create its indexes. History tables come
// first.
func (m *Manager) GetTableInitStatements(
	ctx context.Context,
	tables []*sqlmanager_shared.SchemaTable,
) ([]*sqlmanager_shared.TableInitStatement, error) {
	plan, err := m.plan(ctx, tables)
	if err != nil {
		return nil, err
	}
	return plan.Tables, nil
}

// GetSchemaTableDataTypes gives what the tables need before they are created: the sequences
// their defaults draw from, the alias types of their columns, as domains, and the functions
// they call. SQL Server has no enum, and table types are not reproduced: both lists are empty.
func (m *Manager) GetSchemaTableDataTypes(
	ctx context.Context,
	tables []*sqlmanager_shared.SchemaTable,
) (*sqlmanager_shared.SchemaTableDataTypeResponse, error) {
	plan, err := m.plan(ctx, tables)
	if err != nil {
		return nil, err
	}
	return &sqlmanager_shared.SchemaTableDataTypeResponse{
		Sequences:  plan.Sequences,
		Functions:  plan.Functions,
		Composites: []*sqlmanager_shared.DataType{},
		Enums:      []*sqlmanager_shared.DataType{},
		Domains:    plan.AliasTypes,
	}, nil
}

// GetSequencesByTables gives the sequences the given tables bring with them: those their
// defaults draw from, and those the modules created with them draw from.
func (m *Manager) GetSequencesByTables(
	ctx context.Context,
	schema string,
	tables []string,
) ([]*sqlmanager_shared.DataType, error) {
	requested := make([]*sqlmanager_shared.SchemaTable, len(tables))
	for i, table := range tables {
		requested[i] = &sqlmanager_shared.SchemaTable{Schema: schema, Table: table}
	}
	plan, err := m.plan(ctx, requested)
	if err != nil {
		return nil, err
	}
	return plan.Sequences, nil
}
