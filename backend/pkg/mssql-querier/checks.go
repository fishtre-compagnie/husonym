package mssql_queries

import (
	"context"
	"database/sql"

	mysql_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/mysql"
)

var getCheckConstraints = `-- name: GetCheckConstraints :many
SELECT
    cc.parent_object_id,
    cc.name,
    COALESCE(cc.definition, ''),
    cc.is_disabled,
    cc.is_not_trusted,
    cc.is_not_for_replication
FROM sys.check_constraints cc
WHERE ` + inIDs("cc.parent_object_id") + `
ORDER BY cc.parent_object_id, cc.name;
`

type GetCheckConstraintsRow struct {
	ObjectID            int64
	Name                string
	Definition          string
	IsDisabled          bool
	IsNotTrusted        bool
	IsNotForReplication bool
}

// GetCheckConstraints gives the check constraints of the given tables.
func (q *Queries) GetCheckConstraints(
	ctx context.Context,
	db mysql_queries.DBTX,
	ids []int64,
) ([]*GetCheckConstraintsRow, error) {
	return queryByIDs(ctx, db, getCheckConstraints, ids, func(rows *sql.Rows, i *GetCheckConstraintsRow) error {
		return rows.Scan(
			&i.ObjectID,
			&i.Name,
			&i.Definition,
			&i.IsDisabled,
			&i.IsNotTrusted,
			&i.IsNotForReplication,
		)
	})
}
