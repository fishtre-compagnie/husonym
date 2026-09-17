package genbenthosconfigs_activity

import (
	benthosbuilder "github.com/fishtre-compagnie/husonym/internal/benthos/benthos-builder"
	"github.com/fishtre-compagnie/husonym/internal/tableplan"
)

// toTablePlan extracts the engine-neutral plan of a table sync from the config built
// for Benthos. Only SQL sources have one: other sources are not handled by Athanor.
func toTablePlan(config *benthosbuilder.BenthosConfigResponse) *tableplan.TablePlan {
	if config == nil || config.Config == nil || config.Config.Input == nil {
		return nil
	}
	input := config.Config.Input.PooledSqlRaw
	if input == nil {
		return nil
	}
	plan := &tableplan.TablePlan{
		Id:               config.Name,
		Schema:           config.TableSchema,
		Table:            config.TableName,
		RunType:          config.RunType,
		Query:            input.Query,
		PageQuery:        input.PagedQuery,
		OrderByColumns:   input.OrderByColumns,
		Columns:          config.Columns,
		ForeignKeys:      config.ForeignKeys,
		GeneratedColumns: config.GeneratedColumns,
		PublishedKeys:    config.PublishedKeys,
	}
	if input.ExpectedTotalRows != nil {
		plan.PageLimit = *input.ExpectedTotalRows
	}
	return plan
}
