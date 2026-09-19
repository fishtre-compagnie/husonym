package genbenthosconfigs_activity

import (
	"testing"

	benthosbuilder "github.com/fishtre-compagnie/husonym/internal/benthos/benthos-builder"
	"github.com/fishtre-compagnie/husonym/internal/runconfigs"
	"github.com/fishtre-compagnie/husonym/internal/tableplan"
	husonym_benthos "github.com/fishtre-compagnie/husonym/worker/pkg/benthos"
	"github.com/stretchr/testify/require"
)

func TestToTablePlan(t *testing.T) {
	limit := 100
	config := &benthosbuilder.BenthosConfigResponse{
		Name:        "web.users.update.1",
		TableSchema: "web",
		TableName:   "users",
		RunType:     runconfigs.RunTypeUpdate,
		Columns:     []string{"manager_id"},
		Config: &husonym_benthos.BenthosConfig{StreamConfig: husonym_benthos.StreamConfig{
			Input: &husonym_benthos.InputConfig{Inputs: husonym_benthos.Inputs{
				PooledSqlRaw: &husonym_benthos.InputPooledSqlRaw{
					Query:             "SELECT id, manager_id FROM users LIMIT 100",
					PagedQuery:        "SELECT id, manager_id FROM users WHERE id > ? LIMIT ?",
					ExpectedTotalRows: &limit,
					OrderByColumns:    []string{"id"},
				},
			}},
		}},
	}

	require.Equal(t, &tableplan.TablePlan{
		Id:             "web.users.update.1",
		Schema:         "web",
		Table:          "users",
		RunType:        runconfigs.RunTypeUpdate,
		Query:          "SELECT id, manager_id FROM users LIMIT 100",
		PageQuery:      "SELECT id, manager_id FROM users WHERE id > ? LIMIT ?",
		PageLimit:      100,
		OrderByColumns: []string{"id"},
		Columns:        []string{"manager_id"},
	}, toTablePlan(config))
}

// Non-SQL sources (S3, MongoDB, generate…) have no plan: Athanor does not handle them.
func TestToTablePlan_NonSqlSource(t *testing.T) {
	config := &benthosbuilder.BenthosConfigResponse{
		Name:   "generate",
		Config: &husonym_benthos.BenthosConfig{StreamConfig: husonym_benthos.StreamConfig{Input: &husonym_benthos.InputConfig{}}},
	}
	require.Nil(t, toTablePlan(config))
	require.Nil(t, toTablePlan(nil))
}
