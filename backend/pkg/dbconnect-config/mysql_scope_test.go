package dbconnectconfig

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
)

func mysqlHostConnection(name string) *mgmtv1alpha1.Connection {
	return &mgmtv1alpha1.Connection{
		ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{
			Config: &mgmtv1alpha1.ConnectionConfig_MysqlConfig{
				MysqlConfig: &mgmtv1alpha1.MysqlConnectionConfig{
					ConnectionConfig: &mgmtv1alpha1.MysqlConnectionConfig_Connection{
						Connection: &mgmtv1alpha1.MysqlConnection{Host: "db", Port: 3306, Name: name},
					},
				},
			},
		},
	}
}

func Test_MysqlDatabaseScope(t *testing.T) {
	t.Run("a host connection naming a database is scoped to it", func(t *testing.T) {
		require.Equal(t, "allopneus", MysqlDatabaseScope(mysqlHostConnection(" allopneus ")))
	})
	t.Run("a host connection naming none is not scoped", func(t *testing.T) {
		require.Equal(t, "", MysqlDatabaseScope(mysqlHostConnection("")))
	})
	t.Run("a url connection is not scoped, whatever database its url names", func(t *testing.T) {
		conn := &mgmtv1alpha1.Connection{
			ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{
				Config: &mgmtv1alpha1.ConnectionConfig_MysqlConfig{
					MysqlConfig: &mgmtv1alpha1.MysqlConnectionConfig{
						ConnectionConfig: &mgmtv1alpha1.MysqlConnectionConfig_Url{Url: "u:p@tcp(db:3306)/allopneus"},
					},
				},
			},
		}
		require.Equal(t, "", MysqlDatabaseScope(conn))
	})
	t.Run("a postgres connection is not scoped", func(t *testing.T) {
		conn := &mgmtv1alpha1.Connection{
			ConnectionConfig: &mgmtv1alpha1.ConnectionConfig{
				Config: &mgmtv1alpha1.ConnectionConfig_PgConfig{PgConfig: &mgmtv1alpha1.PostgresConnectionConfig{}},
			},
		}
		require.Equal(t, "", MysqlDatabaseScope(conn))
	})
}

func Test_InMysqlDatabaseScope(t *testing.T) {
	require.True(t, InMysqlDatabaseScope("", "anything"))
	require.True(t, InMysqlDatabaseScope("allopneus", "allopneus"))
	require.True(t, InMysqlDatabaseScope("Allopneus", "allopneus"))
	require.False(t, InMysqlDatabaseScope("allopneus", "alloblog"))
}
