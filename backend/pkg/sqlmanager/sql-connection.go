package sqlmanager

import (
	"github.com/fishtre-compagnie/husonym/backend/pkg/sqldbtx"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
)

type SqlConnection struct {
	database SqlDatabase
	driver   string
	// queryer is the connection the manager asks through, for what it does not ask itself.
	queryer sqldbtx.DBTX
}

// Queryer is the connection the manager asks through: for a question the manager does not
// ask itself, such as whether the account may do what a job needs. Nil for a connection made
// without one.
func (s *SqlConnection) Queryer() sqldbtx.DBTX {
	return s.queryer
}

func (s *SqlConnection) Db() SqlDatabase {
	return s.database
}
func (s *SqlConnection) Driver() string {
	return s.driver
}

func NewPostgresSqlConnection(database SqlDatabase) *SqlConnection {
	return newSqlConnection(database, sqlmanager_shared.PostgresDriver)
}

func NewMysqlSqlConnection(database SqlDatabase) *SqlConnection {
	return newSqlConnection(database, sqlmanager_shared.MysqlDriver)
}

func NewMssqlSqlConnection(database SqlDatabase) *SqlConnection {
	return newSqlConnection(database, sqlmanager_shared.MssqlDriver)
}

func newSqlConnection(database SqlDatabase, driver string) *SqlConnection {
	return &SqlConnection{database: database, driver: driver}
}
