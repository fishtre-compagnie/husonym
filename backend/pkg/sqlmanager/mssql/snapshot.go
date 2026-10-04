package sqlmanager_mssql

import (
	"context"
	"errors"
	"fmt"
	"slices"

	mssql_queries "github.com/fishtre-compagnie/husonym/backend/pkg/mssql-querier"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/mssql/ddl"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/fishtre-compagnie/husonym/internal/backoffutil"
	mssqldb "github.com/microsoft/go-mssqldb"
	"golang.org/x/sync/errgroup"
)

// minCompatibilityLevel is the level from which a database reads a JSON parameter.
const minCompatibilityLevel = 130

// errCatalogChanged tells a read during which the tables or their children changed. What was
// read may mix two states of the catalog: it is read again.
var errCatalogChanged = errors.New("the catalog changed while it was read")

// deadlockVictim is the number of the error a statement gets when the server ends it to let
// another one through.
const deadlockVictim = 1205

// isCatalogChange says whether a read of the catalog failed because the catalog changed under
// it: the versions of the tables moved, or a statement that changes a table and a query that
// reads its catalog waited for each other, and the server ended the query. Read again, the
// catalog tells the state the change left.
func isCatalogChange(err error) bool {
	if errors.Is(err, errCatalogChanged) {
		return true
	}
	var serverError mssqldb.Error
	return errors.As(err, &serverError) && serverError.Number == deadlockVictim
}

// snapshot reads what the catalog says of the tables, as one state of it.
func (m *Manager) snapshot(ctx context.Context, tables []*sqlmanager_shared.SchemaTable) (*ddl.Snapshot, error) {
	info, err := m.querier.GetDatabaseInfo(ctx, m.db)
	if err != nil {
		return nil, fmt.Errorf("unable to read the database: %w", err)
	}
	if info.CompatibilityLevel < minCompatibilityLevel {
		return nil, fmt.Errorf(
			"compatibility level %d: %d or more is required", info.CompatibilityLevel, minCompatibilityLevel,
		)
	}
	// A login without the permission is told no dependency, no sequence and no definition, and
	// no error either: a plan made of that would create something else than the source holds.
	if !info.CanViewDefinitions {
		return nil, errors.New(
			"the login lacks the VIEW DEFINITION permission on the database: the definitions of its objects cannot be read",
		)
	}
	database := ddl.Database{
		CompatibilityLevel: info.CompatibilityLevel,
		Collation:          info.Collation,
		MajorVersion:       info.MajorVersion,
	}

	snapshot, err := backoffutil.Retry(
		ctx,
		func() (*ddl.Snapshot, error) { return m.readSnapshot(ctx, database, tables) },
		m.retryOpts,
		isCatalogChange,
	)
	// A caller who gives up while the manager waits to read again is told so, with the error
	// of its context.
	if err != nil && ctx.Err() != nil && isCatalogChange(err) {
		return nil, fmt.Errorf(
			"gave up reading the catalog, which changed while it was read: %w", context.Cause(ctx),
		)
	}
	if errors.Is(err, errCatalogChanged) {
		return nil, fmt.Errorf(
			"the catalog kept changing while it was read: gave up after %d reads",
			sqlmanager_shared.CatalogReadAttempts,
		)
	}
	return snapshot, err
}

// readSnapshot reads the catalog once. The versions of the tables and of their children are
// read before and after everything else: a difference fails the read with errCatalogChanged.
func (m *Manager) readSnapshot(
	ctx context.Context,
	database ddl.Database,
	tables []*sqlmanager_shared.SchemaTable,
) (*ddl.Snapshot, error) {
	snapshot := &ddl.Snapshot{Database: database}
	if err := m.resolve(ctx, snapshot, tables); err != nil {
		return nil, err
	}
	if len(snapshot.Tables) == 0 {
		return snapshot, nil
	}
	ids := make([]int64, len(snapshot.Tables))
	byID := make(map[int64]*ddl.Table, len(snapshot.Tables))
	for i, table := range snapshot.Tables {
		ids[i] = table.ObjectID
		byID[table.ObjectID] = table
	}

	before, err := m.querier.GetObjectVersions(ctx, m.db, ids)
	if err != nil {
		return nil, fmt.Errorf("unable to read the versions of the tables: %w", err)
	}

	// The headers of the modules and the dependencies tell which definitions and which
	// sequences the tables bring with them.
	if err := m.readSelection(ctx, snapshot); err != nil {
		return nil, err
	}
	wantedModules, wantedSequences := snapshot.Wanted()

	var (
		columns     []*mssql_queries.GetColumnsRow
		indexes     []*mssql_queries.GetIndexesRow
		foreignKeys []*mssql_queries.GetForeignKeysRow
		checks      []*mssql_queries.GetCheckConstraintsRow
		definitions []*mssql_queries.GetModuleDefinitionsRow
		sequences   []*mssql_queries.GetSequencesRow
		notices     []*mssql_queries.GetTableNoticesRow
	)
	errgrp, errctx := errgroup.WithContext(ctx)
	errgrp.Go(func() (err error) {
		columns, err = m.querier.GetColumns(errctx, m.db, ids)
		return wrap("columns", err)
	})
	errgrp.Go(func() (err error) {
		indexes, err = m.querier.GetIndexes(errctx, m.db, ids)
		return wrap("indexes", err)
	})
	errgrp.Go(func() (err error) {
		foreignKeys, err = m.querier.GetForeignKeys(errctx, m.db, ids)
		return wrap("foreign keys", err)
	})
	errgrp.Go(func() (err error) {
		checks, err = m.querier.GetCheckConstraints(errctx, m.db, ids)
		return wrap("check constraints", err)
	})
	errgrp.Go(func() (err error) {
		notices, err = m.querier.GetTableNotices(errctx, m.db, ids, database.MajorVersion)
		return wrap("table attributes", err)
	})
	if len(wantedModules) > 0 {
		errgrp.Go(func() (err error) {
			definitions, err = m.querier.GetModuleDefinitions(errctx, m.db, wantedModules)
			return wrap("module definitions", err)
		})
	}
	if len(wantedSequences) > 0 {
		errgrp.Go(func() (err error) {
			sequences, err = m.querier.GetSequences(errctx, m.db, wantedSequences)
			return wrap("sequences", err)
		})
	}
	if err := errgrp.Wait(); err != nil {
		return nil, err
	}

	after, err := m.querier.GetObjectVersions(ctx, m.db, ids)
	if err != nil {
		return nil, fmt.Errorf("unable to read the versions of the tables: %w", err)
	}
	if !slices.EqualFunc(before, after, func(a, b *mssql_queries.GetObjectVersionsRow) bool {
		return a.ObjectID == b.ObjectID && a.ModifyDate.Equal(b.ModifyDate)
	}) {
		return nil, errCatalogChanged
	}

	for _, row := range columns {
		if table := byID[row.ObjectID]; table != nil {
			table.Columns = append(table.Columns, toColumn(row))
		}
	}
	for id, list := range groupIndexes(indexes) {
		if table := byID[id]; table != nil {
			table.Indexes = list
		}
	}
	for id, list := range groupForeignKeys(foreignKeys) {
		if table := byID[id]; table != nil {
			table.ForeignKeys = list
		}
	}
	for _, row := range checks {
		if table := byID[row.ObjectID]; table != nil {
			table.Checks = append(table.Checks, toCheck(row))
		}
	}
	attachDefinitions(snapshot, wantedModules, definitions)
	for _, row := range sequences {
		snapshot.Sequences = append(snapshot.Sequences, toSequence(row))
	}
	for _, row := range notices {
		snapshot.Notices = append(snapshot.Notices, toNotice(row))
	}
	return snapshot, nil
}

func wrap(what string, err error) error {
	if err != nil {
		return fmt.Errorf("unable to read the %s: %w", what, err)
	}
	return nil
}

// resolve finds the requested names: the tables found, in request order, each once, followed by
// the history tables of the system-versioned ones; the names that are views; the names the
// database does not have, each once.
func (m *Manager) resolve(
	ctx context.Context,
	snapshot *ddl.Snapshot,
	tables []*sqlmanager_shared.SchemaTable,
) error {
	requested := make([]mssql_queries.SchemaTable, len(tables))
	for i, table := range tables {
		requested[i] = mssql_queries.SchemaTable{Schema: table.Schema, Table: table.Table}
	}
	rows, err := m.querier.ResolveTables(ctx, m.db, requested)
	if err != nil {
		return fmt.Errorf("unable to find the tables: %w", err)
	}

	found := make(map[int]bool, len(rows))
	seen := map[int64]bool{}
	add := func(table *ddl.Table) {
		if !seen[table.ObjectID] {
			seen[table.ObjectID] = true
			snapshot.Tables = append(snapshot.Tables, table)
		}
	}
	for _, row := range rows {
		found[row.Position] = true
		if row.IsView {
			view := sqlmanager_shared.SchemaTable{Schema: row.Schema, Table: row.Name}
			if !slices.Contains(snapshot.Views, view) {
				snapshot.Views = append(snapshot.Views, view)
			}
			continue
		}
		add(toTable(row))
	}
	// A system-versioned table brings its history table, whether it was requested or not.
	for _, row := range rows {
		if row.TemporalType == ddl.TemporalSystemVersioned && row.HistoryID != 0 {
			add(toHistoryTable(row))
		}
	}

	for position, table := range tables {
		missing := sqlmanager_shared.SchemaTable{Schema: table.Schema, Table: table.Table}
		if !found[position] && !slices.Contains(snapshot.Missing, missing) {
			snapshot.Missing = append(snapshot.Missing, missing)
		}
	}
	return nil
}

// readSelection reads the headers of every module and every dependency of the database.
func (m *Manager) readSelection(ctx context.Context, snapshot *ddl.Snapshot) error {
	var (
		headers      []*mssql_queries.GetModuleHeadersRow
		dependencies []*mssql_queries.GetDependenciesRow
	)
	errgrp, errctx := errgroup.WithContext(ctx)
	errgrp.Go(func() (err error) {
		headers, err = m.querier.GetModuleHeaders(errctx, m.db)
		return wrap("modules", err)
	})
	errgrp.Go(func() (err error) {
		dependencies, err = m.querier.GetDependencies(errctx, m.db)
		return wrap("dependencies", err)
	})
	if err := errgrp.Wait(); err != nil {
		return err
	}
	for _, row := range headers {
		snapshot.Modules = append(snapshot.Modules, toModule(row))
	}
	for _, row := range dependencies {
		snapshot.Dependencies = append(snapshot.Dependencies, toDependency(row))
	}
	return nil
}
