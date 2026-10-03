package mssql_queries

import (
	"context"
	"database/sql"
	"strings"

	mysql_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/mysql"
)

// Kinds of notices: what a table has of storage and administration.
const (
	NoticeFilegroup          = "filegroup"
	NoticePartitioning       = "partitioning"
	NoticeCompression        = "compression"
	NoticeStatistics         = "statistics"
	NoticeExtendedProperties = "extended properties"
	NoticePermissions        = "permissions"
	NoticeFullTextIndex      = "full-text index"
	NoticeRowLevelSecurity   = "row-level security"
	NoticeChangeTracking     = "change tracking"
	NoticeChangeDataCapture  = "change data capture"
	NoticeTriggerOrder       = "trigger order"
	NoticeColumnstoreOrder   = "columnstore order"
)

// notice writes one branch of the query: a row per table that has the kind, with a name the
// kind found or how many it found. The names of the catalog and its descriptions have
// different collations: both are brought to the database's.
func notice(kind, object, detail, count, from string) string {
	return `SELECT ` + object + ` AS object_id, '` + kind + `' AS kind,
    CAST(` + detail + ` AS nvarchar(128)) COLLATE DATABASE_DEFAULT AS detail, ` + count + ` AS found
FROM ` + from + `
GROUP BY ` + object
}

var tableNotices = []string{
	// A table or an index stored on a filegroup that is not the default one.
	notice(NoticeFilegroup, "i.object_id", "MIN(ds.name)", "0",
		`sys.indexes i JOIN sys.data_spaces ds ON ds.data_space_id = i.data_space_id
WHERE ds.type = 'FG' AND ds.is_default = 0 AND i.is_hypothetical = 0 AND `+inIDs("i.object_id")),
	// A table or an index stored on a partition scheme.
	notice(NoticePartitioning, "i.object_id", "MIN(ds.name)", "0",
		`sys.indexes i JOIN sys.data_spaces ds ON ds.data_space_id = i.data_space_id
WHERE ds.type = 'PS' AND i.is_hypothetical = 0 AND `+inIDs("i.object_id")),
	// Row, page or archive compression: 3 is what every columnstore index has.
	notice(NoticeCompression, "p.object_id", "MIN(p.data_compression_desc)", "0",
		`sys.partitions p
WHERE p.data_compression IN (1, 2, 4) AND `+inIDs("p.object_id")),
	notice(NoticeStatistics, "st.object_id", "''", "COUNT(*)",
		`sys.stats st
WHERE st.user_created = 1 AND `+inIDs("st.object_id")),
	notice(NoticeExtendedProperties, "ep.major_id", "''", "COUNT(*)",
		`sys.extended_properties ep
WHERE ep.class = 1 AND `+inIDs("ep.major_id")),
	notice(NoticePermissions, "dp.major_id", "''", "COUNT(*)",
		`sys.database_permissions dp
WHERE dp.class = 1 AND `+inIDs("dp.major_id")),
	notice(NoticeFullTextIndex, "fi.object_id", "''", "0",
		`sys.fulltext_indexes fi
WHERE `+inIDs("fi.object_id")),
	notice(NoticeRowLevelSecurity, "sp.target_object_id", "''", "COUNT(*)",
		`sys.security_predicates sp
WHERE `+inIDs("sp.target_object_id")),
	notice(NoticeChangeTracking, "ct.object_id", "''", "0",
		`sys.change_tracking_tables ct
WHERE `+inIDs("ct.object_id")),
	notice(NoticeChangeDataCapture, "t.object_id", "''", "0",
		`sys.tables t
WHERE t.is_tracked_by_cdc = 1 AND `+inIDs("t.object_id")),
	// Triggers set to fire first or last among those of their table.
	notice(NoticeTriggerOrder, "tr.parent_id", "''", "COUNT(DISTINCT tr.object_id)",
		`sys.triggers tr JOIN sys.trigger_events te ON te.object_id = tr.object_id
WHERE (te.is_first = 1 OR te.is_last = 1) AND `+inIDs("tr.parent_id")),
}

// columnstoreOrderNotice reads a column that servers before version 16 do not have.
var columnstoreOrderNotice = notice(NoticeColumnstoreOrder, "ic.object_id", "''", "COUNT(DISTINCT ic.index_id)",
	`sys.index_columns ic
WHERE ic.column_store_order_ordinal > 0 AND `+inIDs("ic.object_id"))

// columnstoreOrderVersion is the first major version whose columnstore indexes take an order.
const columnstoreOrderVersion = 16

type GetTableNoticesRow struct {
	ObjectID int64
	Kind     string
	// Detail is a name the kind found, empty when the kind counts.
	Detail string
	// Count is how many the kind found, 0 when the kind names.
	Count int
}

// GetTableNotices tells what the given tables have of storage and administration: one row per
// table and kind. majorVersion is the version of the server, which decides what can be read.
func (q *Queries) GetTableNotices(
	ctx context.Context,
	db mysql_queries.DBTX,
	ids []int64,
	majorVersion int,
) ([]*GetTableNoticesRow, error) {
	branches := tableNotices
	if majorVersion >= columnstoreOrderVersion {
		branches = append(branches[:len(branches):len(branches)], columnstoreOrderNotice)
	}
	query := "-- name: GetTableNotices :many\n" +
		strings.Join(branches, "\nUNION ALL\n") + "\nORDER BY object_id, kind;\n"
	return queryByIDs(ctx, db, query, ids, func(rows *sql.Rows, i *GetTableNoticesRow) error {
		return rows.Scan(&i.ObjectID, &i.Kind, &i.Detail, &i.Count)
	})
}
