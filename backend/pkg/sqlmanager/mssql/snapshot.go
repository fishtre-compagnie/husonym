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
			table.Checks = append(table.Checks, &ddl.CheckConstraint{
				Name:                row.Name,
				Definition:          row.Definition,
				IsDisabled:          row.IsDisabled,
				IsNotTrusted:        row.IsNotTrusted,
				IsNotForReplication: row.IsNotForReplication,
			})
		}
	}
	attachDefinitions(snapshot, wantedModules, definitions)
	for _, row := range sequences {
		snapshot.Sequences = append(snapshot.Sequences, toSequence(row))
	}
	for _, row := range notices {
		snapshot.Notices = append(snapshot.Notices, &ddl.Notice{
			ObjectID: row.ObjectID, Kind: noticeKinds[row.Kind], Detail: row.Detail, Count: row.Count,
		})
	}
	return snapshot, nil
}

func wrap(what string, err error) error {
	if err != nil {
		return fmt.Errorf("unable to read the %s: %w", what, err)
	}
	return nil
}

// resolve finds the requested tables: those found in request order, each once, followed by the
// history tables of the system-versioned ones; those not found, each once.
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
		add(&ddl.Table{
			ObjectID:          row.ObjectID,
			Schema:            row.Schema,
			Name:              row.Name,
			TemporalType:      row.TemporalType,
			HistoryID:         row.HistoryID,
			HistorySchema:     row.HistorySchema,
			HistoryName:       row.HistoryName,
			RetentionPeriod:   row.RetentionPeriod,
			RetentionUnit:     row.RetentionUnit,
			PeriodStartColumn: row.PeriodStartColumn,
			PeriodEndColumn:   row.PeriodEndColumn,
			IsMemoryOptimized: row.IsMemoryOptimized,
			IsFileTable:       row.IsFileTable,
			IsExternal:        row.IsExternal,
			IsNode:            row.IsNode,
			IsEdge:            row.IsEdge,
		})
	}
	// A system-versioned table brings its history table, whether it was requested or not.
	for _, row := range rows {
		if row.TemporalType == ddl.TemporalSystemVersioned && row.HistoryID != 0 {
			add(&ddl.Table{
				ObjectID:     row.HistoryID,
				Schema:       row.HistorySchema,
				Name:         row.HistoryName,
				TemporalType: ddl.TemporalHistory,
			})
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
		snapshot.Modules = append(snapshot.Modules, &ddl.Module{
			ObjectID:             row.ObjectID,
			Schema:               row.Schema,
			Name:                 row.Name,
			Type:                 row.Type,
			ParentID:             row.ParentID,
			UsesAnsiNulls:        row.UsesAnsiNulls,
			UsesQuotedIdentifier: row.UsesQuotedIdentifier,
			IsSchemaBound:        row.IsSchemaBound,
			HasDefinition:        row.HasDefinition,
			IsDisabled:           row.IsDisabled,
		})
	}
	for _, row := range dependencies {
		snapshot.Dependencies = append(snapshot.Dependencies, &ddl.Dependency{
			ReferencingID:            row.ReferencingID,
			ReferencingType:          row.ReferencingType,
			ReferencingParentID:      row.ReferencingParentID,
			ReferencedClass:          row.ReferencedClass,
			ReferencedID:             row.ReferencedID,
			ReferencedType:           row.ReferencedType,
			ReferencedIsTableType:    row.ReferencedIsTableType,
			ReferencedIsAssemblyType: row.ReferencedIsAssemblyType,
			ReferencedSchema:         row.ReferencedSchema,
			ReferencedName:           row.ReferencedName,
		})
	}
	return nil
}

// attachDefinitions gives the wanted modules their text. A wanted module whose text did not
// come back cannot be read: it was dropped or encrypted since its header was read.
func attachDefinitions(
	snapshot *ddl.Snapshot,
	wanted []int64,
	definitions []*mssql_queries.GetModuleDefinitionsRow,
) {
	texts := make(map[int64]string, len(definitions))
	for _, row := range definitions {
		texts[row.ObjectID] = row.Definition
	}
	isWanted := make(map[int64]bool, len(wanted))
	for _, id := range wanted {
		isWanted[id] = true
	}
	for _, module := range snapshot.Modules {
		if !isWanted[module.ObjectID] {
			continue
		}
		text, ok := texts[module.ObjectID]
		module.Definition = text
		module.HasDefinition = module.HasDefinition && ok
	}
}

func toColumn(row *mssql_queries.GetColumnsRow) *ddl.Column {
	base := row.BaseTypeName
	if base == "" && !row.IsUserDefinedType {
		// A system type held by an assembly is stored as itself.
		base = row.TypeName
	}
	return &ddl.Column{
		ColumnID:                  row.ColumnID,
		Name:                      row.Name,
		TypeSchema:                row.TypeSchema,
		TypeName:                  row.TypeName,
		BaseTypeName:              base,
		IsUserDefinedType:         row.IsUserDefinedType,
		IsAssemblyType:            row.IsAssemblyType,
		TypeIsNullable:            row.TypeIsNullable,
		TypeHasRule:               row.TypeRuleID != 0,
		TypeHasDefault:            row.TypeDefaultID != 0,
		MaxLength:                 row.MaxLength,
		Precision:                 row.Precision,
		Scale:                     row.Scale,
		Collation:                 row.Collation,
		IsNullable:                row.IsNullable,
		IsAnsiPadded:              row.IsAnsiPadded,
		IsRowGuidCol:              row.IsRowGuidCol,
		IsFilestream:              row.IsFilestream,
		IsSparse:                  row.IsSparse,
		IsColumnSet:               row.IsColumnSet,
		IsHidden:                  row.IsHidden,
		IsEncrypted:               row.IsEncrypted,
		HasXMLCollection:          row.XMLCollectionID != 0,
		HasRule:                   row.RuleID != 0,
		GeneratedAlways:           row.GeneratedAlways,
		HasDefault:                row.DefaultID != 0,
		DefaultName:               row.DefaultName,
		DefaultDefinition:         row.DefaultDefinition,
		IsComputed:                row.IsComputed,
		ComputedDefinition:        row.ComputedDefinition,
		IsPersisted:               row.IsPersisted,
		IsIdentity:                row.IsIdentity,
		IdentitySeed:              row.IdentitySeed,
		IdentityIncrement:         row.IdentityIncrement,
		IdentityNotForReplication: row.IdentityNotForReplication,
		IsMasked:                  row.IsMasked,
		MaskingFunction:           row.MaskingFunction,
	}
}

// groupIndexes gathers the rows of the index columns into indexes, by table, in index_id order.
func groupIndexes(rows []*mssql_queries.GetIndexesRow) map[int64][]*ddl.Index {
	grouped := map[int64][]*ddl.Index{}
	var current *ddl.Index
	var table int64
	for _, row := range rows {
		if current == nil || table != row.ObjectID || current.IndexID != row.IndexID {
			current = &ddl.Index{
				IndexID:            row.IndexID,
				Name:               row.Name,
				Type:               row.Type,
				IsUnique:           row.IsUnique,
				IsPrimaryKey:       row.IsPrimaryKey,
				IsUniqueConstraint: row.IsUniqueConstraint,
				IsDisabled:         row.IsDisabled,
				IsPadded:           row.IsPadded,
				IgnoreDupKey:       row.IgnoreDupKey,
				AllowRowLocks:      row.AllowRowLocks,
				AllowPageLocks:     row.AllowPageLocks,
				FillFactor:         row.FillFactor,
				HasFilter:          row.HasFilter,
				FilterDefinition:   row.FilterDefinition,
			}
			table = row.ObjectID
			grouped[table] = append(grouped[table], current)
		}
		if row.IndexColumnID != 0 {
			current.Columns = append(current.Columns, &ddl.IndexColumn{
				Name:         row.ColumnName,
				KeyOrdinal:   row.KeyOrdinal,
				IsDescending: row.IsDescending,
				IsIncluded:   row.IsIncluded,
			})
		}
	}
	return grouped
}

// groupForeignKeys gathers the rows of the key columns into foreign keys, by table.
func groupForeignKeys(rows []*mssql_queries.GetForeignKeysRow) map[int64][]*ddl.ForeignKey {
	grouped := map[int64][]*ddl.ForeignKey{}
	var current *ddl.ForeignKey
	var constraint int64
	for _, row := range rows {
		if current == nil || constraint != row.ConstraintID {
			current = &ddl.ForeignKey{
				Name:                row.Name,
				ReferencedID:        row.ReferencedID,
				ReferencedSchema:    row.ReferencedSchema,
				ReferencedTable:     row.ReferencedTable,
				DeleteAction:        row.DeleteAction,
				UpdateAction:        row.UpdateAction,
				IsDisabled:          row.IsDisabled,
				IsNotTrusted:        row.IsNotTrusted,
				IsNotForReplication: row.IsNotForReplication,
			}
			constraint = row.ConstraintID
			grouped[row.ObjectID] = append(grouped[row.ObjectID], current)
		}
		current.Columns = append(current.Columns, &ddl.ForeignKeyColumn{
			Name:           row.ColumnName,
			IsNullable:     row.ColumnIsNullable,
			ReferencedName: row.ReferencedColumn,
		})
	}
	return grouped
}

func toSequence(row *mssql_queries.GetSequencesRow) *ddl.Sequence {
	return &ddl.Sequence{
		ObjectID:          row.ObjectID,
		Schema:            row.Schema,
		Name:              row.Name,
		TypeSchema:        row.TypeSchema,
		TypeName:          row.TypeName,
		BaseTypeName:      row.BaseTypeName,
		IsUserDefinedType: row.IsUserDefinedType,
		TypeIsNullable:    row.TypeIsNullable,
		Precision:         row.Precision,
		Scale:             row.Scale,
		StartValue:        row.StartValue,
		Increment:         row.Increment,
		MinimumValue:      row.MinimumValue,
		MaximumValue:      row.MaximumValue,
		CurrentValue:      row.CurrentValue,
		IsCycling:         row.IsCycling,
		IsCached:          row.IsCached,
		CacheSize:         row.CacheSize,
		IsUsed:            row.IsUsed,
	}
}

// noticeKinds gives, for each kind the querier tells, the kind the generator reports.
var noticeKinds = map[string]string{
	mssql_queries.NoticeFilegroup:          ddl.NoticeFilegroup,
	mssql_queries.NoticePartitioning:       ddl.NoticePartitioning,
	mssql_queries.NoticeCompression:        ddl.NoticeCompression,
	mssql_queries.NoticeStatistics:         ddl.NoticeStatistics,
	mssql_queries.NoticeExtendedProperties: ddl.NoticeExtendedProperties,
	mssql_queries.NoticePermissions:        ddl.NoticePermissions,
	mssql_queries.NoticeFullTextIndex:      ddl.NoticeFullTextIndex,
	mssql_queries.NoticeRowLevelSecurity:   ddl.NoticeRowLevelSecurity,
	mssql_queries.NoticeChangeTracking:     ddl.NoticeChangeTracking,
	mssql_queries.NoticeChangeDataCapture:  ddl.NoticeChangeDataCapture,
	mssql_queries.NoticeTriggerOrder:       ddl.NoticeTriggerOrder,
	mssql_queries.NoticeColumnstoreOrder:   ddl.NoticeColumnstoreOrder,
}
