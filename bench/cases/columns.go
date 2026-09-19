package cases

import (
	"fmt"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/bench/schema"
)

// columnCases hold the columns an INSERT cannot treat like the others: generated,
// invisible, rewritten by the destination on update; and tables with storage options.
func columnCases() []*Case {
	return []*Case{
		generatedColumns("columns-generated-passthrough",
			"Colonnes générées VIRTUAL et STORED laissées en passthrough", nil),
		generatedColumns("columns-generated-default",
			"Colonnes générées VIRTUAL et STORED configurées en valeur par défaut", generateDefault()),
		columnsOnUpdateTimestamp(),
		columnsInvisible(),
		tablePartitioned(),
		tableGeneratedInvisiblePrimaryKey(),
	}
}

func generateDefault() *mgmtv1alpha1.TransformerConfig {
	return &mgmtv1alpha1.TransformerConfig{
		Config: &mgmtv1alpha1.TransformerConfig_GenerateDefaultConfig{GenerateDefaultConfig: &mgmtv1alpha1.GenerateDefault{}},
	}
}

// generatedColumns: a generated column refuses any written value. Whatever the mapping
// says, the destination must end up with the rows of the source, the generated values
// being recomputed from the same inputs.
func generatedColumns(id, title string, transformer *mgmtv1alpha1.TransformerConfig) *Case {
	c := &Case{
		ID:       id,
		Priority: P2,
		Title:    title,
		Tables: []*schema.Table{{
			Name: "LIGNE",
			Columns: []schema.Column{
				{Name: idColumn, Type: schema.Int64()},
				{Name: "prix", Type: schema.Decimal(10, 2)},
				{Name: "qte", Type: schema.Int32()},
				{Name: "total_virtuel", Type: schema.Decimal(12, 2), Nullable: true, GeneratedAs: "prix * qte"},
				{Name: "total_stocke", Type: schema.Decimal(12, 2), Nullable: true, GeneratedAs: "prix * qte", GeneratedStored: true},
			},
			PrimaryKey: []string{idColumn},
		}},
		Seed: func(p Params, emit Emitter) {
			for i := int64(1); i <= 20; i++ {
				emit.Row("LIGNE", []any{i, fmt.Sprintf("%d.50", i), i % 4, nil, nil}, Kept())
			}
		},
	}
	if transformer != nil {
		spec := ColumnSpec{Transformer: transformer, Rules: []Rule{RuleUnchanged}}
		c.Job.Columns = map[string]map[string]ColumnSpec{"LIGNE": {"total_virtuel": spec, "total_stocke": spec}}
	}
	return c
}

// columnsOnUpdateTimestamp: the destination rewrites an ON UPDATE CURRENT_TIMESTAMP
// column whenever a row is updated without naming it. The table is in a subset and has a
// nullable foreign key, the setup in which an engine writes rows in two passes.
func columnsOnUpdateTimestamp() *Case {
	return &Case{
		ID:       "columns-on-update-timestamp",
		Dialects: mysqlOnly,
		Priority: P2,
		Title:    "ON UPDATE CURRENT_TIMESTAMP : date de modification réécrite par une écriture en deux passes",
		Tables: []*schema.Table{
			stationTable(),
			{
				Name: commandeTable,
				Columns: []schema.Column{
					{Name: idColumn, Type: schema.Int64()},
					{Name: stationIDColumn, Type: schema.Int64()},
					{Name: "groupe_id", Type: schema.Int64(), Nullable: true},
					{
						Name: "modifie_le", Type: mysqlType("TIMESTAMP(6)"),
						Default: "CURRENT_TIMESTAMP(6)", OnUpdate: "CURRENT_TIMESTAMP(6)",
					},
				},
				PrimaryKey: []string{idColumn},
				ForeignKeys: []schema.ForeignKey{
					foreignKeyToID("fk_commande_station", stationIDColumn, "STATION"),
					foreignKeyToID("fk_commande_groupe", "groupe_id", commandeTable),
				},
			},
		},
		Job: subsetOnStationJob(),
		Seed: func(p Params, emit Emitter) {
			seedStations(emit)
			for i := int64(1); i <= 20; i++ {
				var groupe any
				if i > 1 {
					groupe = i - 1
				}
				emit.Row(commandeTable, []any{i, stationKept, groupe, "2020-01-02 03:04:05.678901"}, Kept())
			}
		},
	}
}

// columnsInvisible: SELECT * leaves an invisible column out; naming it brings it back.
func columnsInvisible() *Case {
	return &Case{
		ID:       "columns-invisible",
		Dialects: mysqlOnly,
		Priority: P2,
		Title:    "Colonne INVISIBLE (absente de SELECT *)",
		Tables: []*schema.Table{{
			Name: "COMPTE",
			Columns: []schema.Column{
				{Name: idColumn, Type: schema.Int64()},
				{Name: "nom", Type: schema.Varchar(40)},
				{Name: "secret", Type: schema.Varchar(40), Nullable: true, Invisible: true},
			},
			PrimaryKey: []string{idColumn},
		}},
		Seed: func(p Params, emit Emitter) {
			for i := int64(1); i <= 10; i++ {
				emit.Row("COMPTE", []any{i, fmt.Sprintf("compte %d", i), fmt.Sprintf("secret %d", i)}, Kept())
			}
		},
	}
}

// tablePartitioned: a partitioned table, paged across its partitions.
func tablePartitioned() *Case {
	return &Case{
		ID:       "table-partitioned",
		Dialects: mysqlOnly,
		Priority: P2,
		Title:    "Table partitionnée (PARTITION BY HASH), paginée",
		Tables: []*schema.Table{{
			Name: "MESURE",
			Columns: []schema.Column{
				{Name: idColumn, Type: schema.Int64()},
				{Name: "valeur", Type: schema.Int32()},
			},
			PrimaryKey: []string{idColumn},
			Options:    "PARTITION BY HASH(id) PARTITIONS 4",
		}},
		Seed: func(p Params, emit Emitter) {
			for i := int64(1); i <= int64(2*p.PageLimit+7); i++ {
				emit.Row("MESURE", []any{i, i % 100}, Kept())
			}
		},
	}
}

// tableGeneratedInvisiblePrimaryKey: a table declared without a primary key, to which the
// server adds one of its own (sql_generate_invisible_primary_key, MySQL 8.0.30): an
// invisible my_row_id numbered by the server, on the source and on the destination. The
// job knows the columns the table was declared with; the key it pages on, if it takes it,
// is none of them.
func tableGeneratedInvisiblePrimaryKey() *Case {
	return &Case{
		ID:       "table-generated-invisible-primary-key",
		Dialects: mysqlOnly,
		Priority: P1,
		Title:    "Table sans clé à laquelle MySQL ajoute une clé primaire invisible (sql_generate_invisible_primary_key)",
		SchemaSetupFor: map[schema.Dialect][]string{
			schema.MySQL: {"SET SESSION sql_generate_invisible_primary_key = ON"},
		},
		Tables: []*schema.Table{{
			Name: "JOURNAL",
			Columns: []schema.Column{
				{Name: "niveau", Type: schema.Int32()},
				{Name: "message", Type: schema.Varchar(60)},
			},
		}},
		Seed: func(p Params, emit Emitter) {
			for i := 1; i <= 2*p.PageLimit+p.PageLimit/2; i++ {
				emit.Row("JOURNAL", []any{int64(i % 7), fmt.Sprintf("événement %05d", i)}, Kept())
			}
		},
	}
}
