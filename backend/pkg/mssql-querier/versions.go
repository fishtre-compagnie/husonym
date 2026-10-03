package mssql_queries

import (
	"context"
	"database/sql"
	"time"

	mysql_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/mysql"
)

var getObjectVersions = `-- name: GetObjectVersions :many
SELECT o.object_id, o.modify_date
FROM sys.objects o
WHERE ` + inIDs("o.object_id") + ` OR ` + inIDs("o.parent_object_id") + `
ORDER BY o.object_id;
`

type GetObjectVersionsRow struct {
	ObjectID   int64
	ModifyDate time.Time
}

// GetObjectVersions tells when the given objects and their children — constraints, triggers —
// were last modified. Two answers that differ tell a catalog that changed in between.
func (q *Queries) GetObjectVersions(
	ctx context.Context,
	db mysql_queries.DBTX,
	ids []int64,
) ([]*GetObjectVersionsRow, error) {
	return queryByIDs(ctx, db, getObjectVersions, ids, func(rows *sql.Rows, i *GetObjectVersionsRow) error {
		return rows.Scan(&i.ObjectID, &i.ModifyDate)
	})
}
