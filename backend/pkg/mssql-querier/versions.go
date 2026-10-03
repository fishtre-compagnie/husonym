package mssql_queries

import (
	"context"
	"database/sql"
	"time"

	mysql_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/mysql"
)

// The ids are put in a keyed table first, and each half of the answer joins it: the objects
// themselves by their id, their children by the id of their parent. The cost grows with the
// number of objects, whatever the number of ids.
const getObjectVersions = `-- name: GetObjectVersions :many
SET NOCOUNT ON;
DECLARE @requested TABLE (id int NOT NULL PRIMARY KEY);
INSERT INTO @requested (id) SELECT DISTINCT CAST(value AS int) FROM OPENJSON(@ids);
SELECT o.object_id, o.modify_date
FROM @requested r
JOIN sys.objects o ON o.object_id = r.id
UNION ALL
SELECT o.object_id, o.modify_date
FROM @requested r
JOIN sys.objects o ON o.parent_object_id = r.id
ORDER BY 1
OPTION (RECOMPILE);
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
