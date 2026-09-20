// Package env describes where the bench runs: the Husonym API, the frozen source server
// and one destination server per engine.
//
// Every server has two addresses: the one the bench reaches from the host, and the one
// the worker reaches from inside the compose network.
package env

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strconv"

	"github.com/fishtre-compagnie/husonym/bench/cases"
	"github.com/fishtre-compagnie/husonym/bench/schema"
	"github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib" // the bench opens PostgreSQL servers through pgx
)

// Engine is one of the two engines under comparison.
type Engine string

const (
	Benthos Engine = "benthos"
	Athanor Engine = "athanor"
)

// Engines lists the engines in the order they are run and reported.
var Engines = []Engine{Benthos, Athanor}

// Server is one database server of the bench.
type Server struct {
	// Dialect is the database it speaks.
	Dialect schema.Dialect
	// Addr is host:port as seen from the bench.
	Addr string
	// WorkerHost and WorkerPort are the address as seen from the worker.
	WorkerHost string
	WorkerPort int32
	User       string
	Password   string
	// Database is the database of the connection, the one holding the schemas of the
	// cases. On MySQL a case is a database of its own, so this one only opens the session.
	Database string
}

// Open connects to the server from the bench. Values are read as the text the database
// prints (no time parsing): that text is what rows are compared in.
func (s *Server) Open() (*sql.DB, error) {
	driver, dsn := "mysql", ""
	switch s.Dialect {
	case schema.MySQL:
		cfg := mysql.NewConfig()
		cfg.Net = "tcp"
		cfg.Addr = s.Addr
		cfg.User = s.User
		cfg.Passwd = s.Password
		cfg.Params = map[string]string{"charset": "utf8mb4"}
		dsn = cfg.FormatDSN()
	case schema.Postgres:
		driver = "pgx"
		dsn = (&url.URL{
			Scheme:   "postgres",
			User:     url.UserPassword(s.User, s.Password),
			Host:     s.Addr,
			Path:     "/" + s.Database,
			RawQuery: "sslmode=disable",
		}).String()
	default:
		return nil, fmt.Errorf("env: %s: unknown dialect %q", s.Addr, s.Dialect)
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("env: %s: %w", s.Addr, err)
	}
	return db, nil
}

// Env is the whole bench environment.
type Env struct {
	// Dialect is the database this pass runs on: one pass exercises one database, and
	// its verdicts are recorded under it.
	Dialect      schema.Dialect
	APIURL       string
	Source       Server
	Destinations map[Engine]Server
	Params       cases.Params
}

// defaults are what bench/compose.bench.yml starts next to the development stack, per
// database: the port of each server on the host, and the account the bench opens them
// with. A PostgreSQL superuser is needed to suspend foreign keys while a seed is loaded.
type defaults struct {
	ports map[string]int
	// containerPrefix names the compose services of this database; the two sets run side
	// by side, so only one of them can carry the plain name.
	containerPrefix string
	workerPort      int32
	user            string
	database        string
	passwordVar     string
}

//nolint:gosec // passwordVar names the environment variable, it holds no password
var defaultsByDialect = map[schema.Dialect]defaults{
	schema.MySQL: {
		ports:           map[string]int{"SOURCE": 3309, "DEST_BENTHOS": 3310, "DEST_ATHANOR": 3311},
		containerPrefix: "husonym-bench-",
		workerPort:      3306,
		user:            "root",
		database:        "mysql",
		passwordVar:     "BENCH_MYSQL_PASSWORD",
	},
	schema.Postgres: {
		ports:           map[string]int{"SOURCE": 3312, "DEST_BENTHOS": 3313, "DEST_ATHANOR": 3314},
		containerPrefix: "husonym-bench-pg-",
		workerPort:      5432,
		user:            "postgres",
		database:        "bench",
		passwordVar:     "BENCH_PG_PASSWORD",
	},
}

var workerHostSuffix = map[string]string{
	"SOURCE":       "source",
	"DEST_BENTHOS": "dest-benthos",
	"DEST_ATHANOR": "dest-athanor",
}

// FromEnvironment reads the environment, defaulting to what bench/compose.bench.yml
// starts next to the development stack.
func FromEnvironment() (*Env, error) {
	pageLimit, err := intVar("BENCH_PAGE_LIMIT", 100)
	if err != nil {
		return nil, err
	}
	scale, err := intVar("BENCH_SCALE", 1)
	if err != nil {
		return nil, err
	}
	dialect, err := schema.ParseDialect(stringVar("BENCH_DIALECT", string(schema.MySQL)))
	if err != nil {
		return nil, fmt.Errorf("env: BENCH_DIALECT: %w", err)
	}
	d := defaultsByDialect[dialect]
	password := stringVar(d.passwordVar, "bench")
	server := func(name string) Server {
		return Server{
			Dialect:    dialect,
			Addr:       stringVar("BENCH_"+name+"_ADDR", "127.0.0.1:"+strconv.Itoa(d.ports[name])),
			WorkerHost: stringVar("BENCH_"+name+"_WORKER_HOST", d.containerPrefix+workerHostSuffix[name]),
			WorkerPort: d.workerPort,
			User:       d.user,
			Password:   password,
			Database:   d.database,
		}
	}
	return &Env{
		Dialect: dialect,
		APIURL:  stringVar("BENCH_API_URL", "http://localhost:8080"),
		Source:  server("SOURCE"),
		Destinations: map[Engine]Server{
			Benthos: server("DEST_BENTHOS"),
			Athanor: server("DEST_ATHANOR"),
		},
		Params: cases.Params{PageLimit: pageLimit, Scale: scale, Dialect: dialect},
	}, nil
}

func stringVar(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func intVar(name string, fallback int) (int, error) {
	v := os.Getenv(name)
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("env: %s must be a positive integer, got %q", name, v)
	}
	return n, nil
}
