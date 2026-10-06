package connectiondata

import (
	"bytes"
	"encoding/gob"
	"testing"

	"connectrpc.com/connect"
	"github.com/DATA-DOG/go-sqlmock"
	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/stretchr/testify/require"
)

// rowCollector keeps the rows a sample sends.
type rowCollector struct{ rows []map[string]any }

func (c *rowCollector) Send(resp *mgmtv1alpha1.GetConnectionDataStreamResponse) error {
	var row map[string]any
	if err := gob.NewDecoder(bytes.NewReader(resp.GetRowBytes())).Decode(&row); err != nil {
		return err
	}
	c.rows = append(c.rows, row)
	return nil
}

func columnCatalog() []*sqlmanager_shared.DatabaseSchemaRow {
	return []*sqlmanager_shared.DatabaseSchemaRow{
		{TableSchema: "sch", TableName: "tbl", ColumnName: "id", DataType: "integer"},
		{TableSchema: "sch", TableName: "tbl", ColumnName: "email", DataType: "character varying"},
	}
}

func Test_SampleColumn_ReadsTheFilledValuesOfATextColumn(t *testing.T) {
	service, dbMock := catalogService(t, columnCatalog(), true)
	dbMock.ExpectQuery(`SELECT * FROM (SELECT "email" FROM (SELECT "email" FROM "sch"."tbl" LIMIT 20000) AS "husonym_scan" WHERE (("email" IS NOT NULL) AND ("email" <> '')) LIMIT 1000) AS "husonym_sample" ORDER BY RANDOM() ASC LIMIT 5`).
		WillReturnRows(sqlmock.NewRows([]string{"email"}).AddRow("a@example.com").AddRow("b@example.com"))
	stream := &rowCollector{}

	err := service.SampleColumn(t.Context(), stream, "sch", "tbl", "email", 5)

	require.NoError(t, err)
	require.Equal(t, []map[string]any{{"email": "a@example.com"}, {"email": "b@example.com"}}, stream.rows)
}

func Test_SampleColumn_LeavesOutNullOnlyForAColumnThatIsNotText(t *testing.T) {
	service, dbMock := catalogService(t, columnCatalog(), true)
	dbMock.ExpectQuery(`SELECT * FROM (SELECT "id" FROM (SELECT "id" FROM "sch"."tbl" LIMIT 20000) AS "husonym_scan" WHERE ("id" IS NOT NULL) LIMIT 1000) AS "husonym_sample" ORDER BY RANDOM() ASC LIMIT 5`).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	err := service.SampleColumn(t.Context(), &rowCollector{}, "sch", "tbl", "id", 5)

	require.NoError(t, err)
}

func Test_SampleColumn_ColumnAbsentFromTheCatalogue(t *testing.T) {
	// No connection is opened and no statement is sent: the catalogue is the only read.
	service, _ := catalogService(t, columnCatalog(), false)

	err := service.SampleColumn(t.Context(), &rowCollector{}, "sch", "tbl", "missing", 5)

	require.Error(t, err)
	require.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
	require.NotContains(t, err.Error(), "SELECT")
}

func Test_SampleColumn_TableAbsentFromTheCatalogue(t *testing.T) {
	service, _ := catalogService(t, columnCatalog(), false)

	err := service.SampleColumn(t.Context(), &rowCollector{}, "sch", "other", "email", 5)

	require.Error(t, err)
}

func Test_HoldsText(t *testing.T) {
	for _, dataType := range []string{
		"text", "character varying", "character", "citext", "CHARACTER VARYING",
	} {
		require.True(t, holdsText(sqlmanager_shared.GoquPostgresDriver, dataType), dataType)
	}
	for _, dataType := range []string{
		"char", "varchar", "tinytext", "text", "mediumtext", "longtext", "varchar(255)", "char(3)", "Varchar(40)",
	} {
		require.True(t, holdsText(sqlmanager_shared.MysqlDriver, dataType), dataType)
	}
	for _, dataType := range []string{"varchar", "nvarchar(max)", "char(10)", "nchar", "NVARCHAR(50)"} {
		require.True(t, holdsText(sqlmanager_shared.MssqlDriver, dataType), dataType)
	}
	// SQL Server refuses <> on its legacy text types: they are filtered on NULL only.
	for _, dataType := range []string{"text", "ntext", "NTEXT"} {
		require.False(t, holdsText(sqlmanager_shared.MssqlDriver, dataType), dataType)
	}
	// A length is no reason to refuse a type, an array suffix is: an array takes no comparison
	// with the empty string.
	require.True(t, holdsText(sqlmanager_shared.GoquPostgresDriver, "character varying(20)"))
	require.True(t, holdsText(sqlmanager_shared.GoquPostgresDriver, "character(3) "))
	for _, dataType := range []string{
		"character varying(20)[]", "character(3)[]", "character varying[]", "text[]", "character varying(20)[][]",
	} {
		require.False(t, holdsText(sqlmanager_shared.GoquPostgresDriver, dataType), dataType)
	}
	// A type name of one engine is no text type of another.
	require.False(t, holdsText(sqlmanager_shared.GoquPostgresDriver, "varchar"))
	require.False(t, holdsText(sqlmanager_shared.GoquPostgresDriver, "nvarchar"))
	require.False(t, holdsText(sqlmanager_shared.MysqlDriver, "character varying"))
	require.False(t, holdsText(sqlmanager_shared.MysqlDriver, "citext"))
	require.False(t, holdsText(sqlmanager_shared.MysqlDriver, "nvarchar"))
	require.False(t, holdsText(sqlmanager_shared.MssqlDriver, "citext"))
	require.False(t, holdsText(sqlmanager_shared.MssqlDriver, "character varying"))
	require.False(t, holdsText(sqlmanager_shared.MssqlDriver, "longtext"))
	for _, dataType := range []string{
		"integer", "bigint", "uuid", "json", "jsonb", "bytea", "boolean", "timestamp without time zone",
		"enum", "enum('a','b')", "set('x')", "varbinary(20)", "binary", "blob", "int", "date", "xml", "", "name",
	} {
		for _, driver := range []string{
			sqlmanager_shared.GoquPostgresDriver, sqlmanager_shared.MysqlDriver, sqlmanager_shared.MssqlDriver,
		} {
			require.False(t, holdsText(driver, dataType), "%s %s", driver, dataType)
		}
	}
}
