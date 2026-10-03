package mssql_queries

import (
	"context"
	"database/sql"
	"strings"

	mysql_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/mysql"
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/mssql/ddl"
)

// tableNotice is one branch of the query of the notices: what tells one kind.
type tableNotice struct {
	// sinceVersion is the first major version of the server that can be asked, 0 for all.
	sinceVersion int
	query        string
}

// notice writes one branch of the query: a row per table that has the kind, with a name the
// kind found or how many it found. The names of the catalog and its descriptions have
// different collations: both are brought to the database's.
func notice(kind, object, detail, count, from string) tableNotice {
	return tableNotice{query: `SELECT ` + object + ` AS object_id, '` + kind + `' AS kind,
    CAST(` + detail + ` AS nvarchar(128)) COLLATE DATABASE_DEFAULT AS detail, ` + count + ` AS found
FROM ` + from + `
GROUP BY ` + object}
}

func (n tableNotice) since(majorVersion int) tableNotice {
	n.sinceVersion = majorVersion
	return n
}

// Storage and administration, then the options that are told when they are not at their
// default.
var tableNotices = []tableNotice{
	// A table or an index stored on a filegroup that is not the default one.
	notice(ddl.NoticeFilegroup, "i.object_id", "MIN(ds.name)", "0",
		`sys.indexes i JOIN sys.data_spaces ds ON ds.data_space_id = i.data_space_id
WHERE ds.type = 'FG' AND ds.is_default = 0 AND i.is_hypothetical = 0 AND `+inIDs("i.object_id")),
	// A table or an index stored on a partition scheme.
	notice(ddl.NoticePartitioning, "i.object_id", "MIN(ds.name)", "0",
		`sys.indexes i JOIN sys.data_spaces ds ON ds.data_space_id = i.data_space_id
WHERE ds.type = 'PS' AND i.is_hypothetical = 0 AND `+inIDs("i.object_id")),
	// Row, page or archive compression: 3 is what every columnstore index has.
	notice(ddl.NoticeCompression, "p.object_id", "MIN(p.data_compression_desc)", "0",
		`sys.partitions p
WHERE p.data_compression IN (1, 2, 4) AND `+inIDs("p.object_id")),
	notice(ddl.NoticeXMLCompression, "p.object_id", "''", "0",
		`sys.partitions p
WHERE p.xml_compression = 1 AND `+inIDs("p.object_id")).since(16),
	notice(ddl.NoticeStatistics, "st.object_id", "''", "COUNT(*)",
		`sys.stats st
WHERE st.user_created = 1 AND `+inIDs("st.object_id")),
	// The properties of a table, of its columns and of its indexes name the table; those of
	// its constraints and of its triggers name the constraint or the trigger.
	notice(ddl.NoticeExtendedProperties, "owner.id", "''", "COUNT(*)",
		`(
    SELECT ep.major_id AS id
    FROM sys.extended_properties ep
    WHERE ep.class IN (1, 7) AND `+inIDs("ep.major_id")+`
    UNION ALL
    SELECT o.parent_object_id
    FROM sys.extended_properties ep
    JOIN sys.objects o ON o.object_id = ep.major_id
    WHERE ep.class = 1 AND `+inIDs("o.parent_object_id")+`
) owner`),
	notice(ddl.NoticePermissions, "dp.major_id", "''", "COUNT(*)",
		`sys.database_permissions dp
WHERE dp.class = 1 AND `+inIDs("dp.major_id")),
	notice(ddl.NoticeFullTextIndex, "fi.object_id", "''", "0",
		`sys.fulltext_indexes fi
WHERE `+inIDs("fi.object_id")),
	notice(ddl.NoticeRowLevelSecurity, "sp.target_object_id", "''", "COUNT(*)",
		`sys.security_predicates sp
WHERE `+inIDs("sp.target_object_id")),
	notice(ddl.NoticeChangeTracking, "ct.object_id", "''", "0",
		`sys.change_tracking_tables ct
WHERE `+inIDs("ct.object_id")),
	notice(ddl.NoticeChangeDataCapture, "t.object_id", "''", "0",
		`sys.tables t
WHERE t.is_tracked_by_cdc = 1 AND `+inIDs("t.object_id")),
	// Triggers set to fire first or last among those of their table.
	notice(ddl.NoticeTriggerOrder, "tr.parent_id", "''", "COUNT(DISTINCT tr.object_id)",
		`sys.triggers tr JOIN sys.trigger_events te ON te.object_id = tr.object_id
WHERE (te.is_first = 1 OR te.is_last = 1) AND `+inIDs("tr.parent_id")),
	notice(ddl.NoticeColumnstoreOrder, "ic.object_id", "''", "COUNT(DISTINCT ic.index_id)",
		`sys.index_columns ic
WHERE ic.column_store_order_ordinal > 0 AND `+inIDs("ic.object_id")).since(16),
	notice(ddl.NoticeSequentialKey, "i.object_id", "''", "COUNT(*)",
		`sys.indexes i
WHERE i.optimize_for_sequential_key = 1 AND `+inIDs("i.object_id")).since(15),
	// The statistics of the indexes that are not computed again by themselves.
	notice(ddl.NoticeStatisticsNoRecompute, "st.object_id", "''", "COUNT(*)",
		`sys.stats st
WHERE st.no_recompute = 1 AND st.user_created = 0 AND st.auto_created = 0 AND `+inIDs("st.object_id")),
	notice(ddl.NoticeLockEscalation, "t.object_id", "MIN(t.lock_escalation_desc)", "0",
		`sys.tables t
WHERE t.lock_escalation <> 0 AND `+inIDs("t.object_id")),
	notice(ddl.NoticeTextInRow, "t.object_id", "MIN(t.text_in_row_limit)", "0",
		`sys.tables t
WHERE t.text_in_row_limit <> 0 AND `+inIDs("t.object_id")),
	notice(ddl.NoticeLargeValuesOutOfRow, "t.object_id", "''", "0",
		`sys.tables t
WHERE t.large_value_types_out_of_row = 1 AND `+inIDs("t.object_id")),
}

type GetTableNoticesRow struct {
	ObjectID int64
	// Kind is one of the notice kinds of the ddl package.
	Kind string
	// Detail is a name the kind found, empty when the kind counts.
	Detail string
	// Count is how many the kind found, 0 when the kind names.
	Count int
}

// GetTableNotices tells what the given tables have of storage, administration and options:
// one row per table and kind. majorVersion is the version of the server, which decides what
// can be asked.
func (q *Queries) GetTableNotices(
	ctx context.Context,
	db mysql_queries.DBTX,
	ids []int64,
	majorVersion int,
) ([]*GetTableNoticesRow, error) {
	branches := []string{}
	for _, branch := range tableNotices {
		if majorVersion >= branch.sinceVersion {
			branches = append(branches, branch.query)
		}
	}
	query := "-- name: GetTableNotices :many\n" +
		strings.Join(branches, "\nUNION ALL\n") + "\nORDER BY object_id, kind;\n"
	return queryByIDs(ctx, db, query, ids, func(rows *sql.Rows, i *GetTableNoticesRow) error {
		return rows.Scan(&i.ObjectID, &i.Kind, &i.Detail, &i.Count)
	})
}
