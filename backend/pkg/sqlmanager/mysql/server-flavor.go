package sqlmanager_mysql

import (
	"regexp"
	"strconv"
	"strings"

	mysql_queries "github.com/fishtre-compagnie/husonym/backend/gen/go/db/dbschemas/mysql"
)

// serverFlavor is the kind of server the manager reads its schema from. The catalog of each
// one differs in ways the generated queries, written for MySQL 8, do not handle alone.
type serverFlavor int

const (
	// flavorMysql is MySQL 8.0 and later, and any server whose version cannot be read: the
	// generated queries are used as they are.
	flavorMysql serverFlavor = iota
	// flavorMariadb is MariaDB, whose rows are normalized by mariadbQuerier.
	flavorMariadb
	// flavorMysql57 is MySQL before 8.0 (5.7 in practice), read through mysql57Querier.
	flavorMysql57
)

// mysqlVersionPrefix reads the major and minor numbers at the start of VERSION(), which
// carries a suffix on most builds: 5.7.42-log, 8.0.36-0ubuntu0.22.04.1, 5.7.42-46 (Percona).
var mysqlVersionPrefix = regexp.MustCompile(`^\s*(\d+)\.(\d+)`)

// detectServerFlavor tells the flavor of a server from what SELECT VERSION() answers. A
// version it cannot read is taken for MySQL 8, which keeps the behavior the manager had
// before it looked at the version at all.
func detectServerFlavor(version string) serverFlavor {
	if strings.Contains(strings.ToLower(version), "mariadb") {
		return flavorMariadb
	}
	match := mysqlVersionPrefix.FindStringSubmatch(version)
	if match == nil {
		return flavorMysql
	}
	major, err := strconv.Atoi(match[1])
	if err != nil {
		return flavorMysql
	}
	if major < 8 {
		return flavorMysql57
	}
	return flavorMysql
}

// querierForFlavor wraps the generated querier with the normalization the flavor needs.
func querierForFlavor(inner mysql_queries.Querier, flavor serverFlavor) mysql_queries.Querier {
	switch flavor {
	case flavorMariadb:
		return newMariadbQuerier(inner)
	case flavorMysql57:
		return newMysql57Querier(inner)
	default:
		return inner
	}
}

// createsRoutinesIfNotExists says whether the server takes CREATE TRIGGER IF NOT EXISTS and
// CREATE FUNCTION IF NOT EXISTS (MySQL 8.0.29+, MariaDB). MySQL 5.7 takes neither, and the
// statements read from it are written to drop and create the object instead.
func (f serverFlavor) createsRoutinesIfNotExists() bool {
	return f != flavorMysql57
}

// mysqlVersionNumbers reads major.minor.patch at the start of VERSION().
var mysqlVersionNumbers = regexp.MustCompile(`^\s*(\d+)\.(\d+)\.(\d+)`)

// CreatesTriggersIfNotExists says whether a server, given by what SELECT VERSION() answers,
// takes CREATE TRIGGER IF NOT EXISTS: MySQL from 8.0.29 on, and MariaDB. A version it cannot
// read is taken for one that does, which is what was assumed before the version was asked.
func CreatesTriggersIfNotExists(version string) bool {
	if detectServerFlavor(version) == flavorMariadb {
		return true
	}
	match := mysqlVersionNumbers.FindStringSubmatch(version)
	if match == nil {
		return true
	}
	numbers := make([]int, 3)
	for i := range numbers {
		n, err := strconv.Atoi(match[i+1])
		if err != nil {
			return true
		}
		numbers[i] = n
	}
	major, minor, patch := numbers[0], numbers[1], numbers[2]
	switch {
	case major != 8:
		return major > 8
	case minor != 0:
		return true
	default:
		return patch >= 29
	}
}
