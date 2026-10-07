package connectionchecks

import (
	"context"
	"regexp"
	"strconv"
)

// The engines majorOf knows.
const (
	enginePostgres = "postgres"
	engineMysql    = "mysql"
)

// VersionMajor is the major version of the server behind a connection, as "16" for PostgreSQL
// and "8.0" for MySQL; empty when it cannot be read, which is never an error: nothing a run
// does depends on it.
func VersionMajor(ctx context.Context, db Db, dialect Dialect) string {
	switch dialect {
	case Postgres:
		return majorOf(enginePostgres, strconv.Itoa(postgresVersion(ctx, db)))
	case MySQL:
		return majorOf(engineMysql, mysqlVersion(ctx, db))
	}
	return ""
}

// mysqlMajor reads the two first numbers of what VERSION() gives, such as 8.0.36-log.
var mysqlMajor = regexp.MustCompile(`^(\d{1,3})\.(\d{1,3})(?:\D|$)`)

// majorOf reduces the version an engine gives of itself to its major version; empty for an
// engine it does not know, or a version it cannot read. PostgreSQL gives server_version_num,
// MySQL the text of VERSION(). What it returns is one or two numbers of three digits at most,
// and nothing else of what the server said.
func majorOf(engine, raw string) string {
	switch engine {
	case enginePostgres:
		number, err := strconv.Atoi(raw)
		if err != nil || number < 10000 || number >= 1000*10000 {
			return ""
		}
		return strconv.Itoa(number / 10000)
	case engineMysql:
		numbers := mysqlMajor.FindStringSubmatch(raw)
		if numbers == nil {
			return ""
		}
		return numbers[1] + "." + numbers[2]
	}
	return ""
}
