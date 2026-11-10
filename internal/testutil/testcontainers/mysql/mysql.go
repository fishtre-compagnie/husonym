package testcontainers_mysql

import (
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/fishtre-compagnie/husonym/internal/sshtunnel/connectors/mysqltunconnector"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/testcontainers/testcontainers-go"
	testmysql "github.com/testcontainers/testcontainers-go/modules/mysql"
	"github.com/testcontainers/testcontainers-go/wait"
	"golang.org/x/sync/errgroup"
)

type MysqlTestSyncContainer struct {
	Source *MysqlTestContainer
	Target *MysqlTestContainer
}

func NewMysqlTestSyncContainer(
	ctx context.Context,
	sourceOpts, destOpts []Option,
) (*MysqlTestSyncContainer, error) {
	tc := &MysqlTestSyncContainer{}
	errgrp := errgroup.Group{}
	errgrp.Go(func() error {
		m, err := NewMysqlTestContainer(ctx, sourceOpts...)
		if err != nil {
			return err
		}
		tc.Source = m
		return nil
	})

	errgrp.Go(func() error {
		m, err := NewMysqlTestContainer(ctx, destOpts...)
		if err != nil {
			return err
		}
		tc.Target = m
		return nil
	})

	err := errgrp.Wait()
	if err != nil {
		return nil, err
	}

	return tc, nil
}

func (m *MysqlTestSyncContainer) TearDown(ctx context.Context) error {
	if m.Source != nil {
		err := m.Source.TearDown(ctx)
		if err != nil {
			return err
		}
	}
	if m.Target != nil {
		err := m.Target.TearDown(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

const (
	defaultMysqlImage = "mysql:8.4.11"

	// Mysql57Image is the MySQL 5.7 server of the 5.7 variant of the tests, the version of the
	// legacy production servers. See WithMysql57.
	Mysql57Image = "mysql:5.7.42"

	// The same line ends the start of 5.7, 8.0 and 8.4: "Version: '5.7.42'  socket: '...'
	// port: 3306  MySQL Community Server (GPL)". The temporary server of the entrypoint
	// listens on no port ("port: 0"), so the line only matches the real one.
	mysqlReadyLog   = "port: 3306  MySQL Community Server"
	mariadbReadyLog = "port: 3306  mariadb.org binary distribution"
)

// mysql57ServerArgs configure a MySQL 5.7 server like the legacy production ones: no SQL mode
// (zero dates and silent truncation are accepted), TIMESTAMP columns with the implicit
// defaults of the old behavior (the first one of a table takes DEFAULT CURRENT_TIMESTAMP ON
// UPDATE CURRENT_TIMESTAMP, the next ones '0000-00-00 00:00:00'), and utf8 (3 bytes) as the
// character set of the server.
var mysql57ServerArgs = []string{
	"--sql-mode=",
	"--explicit-defaults-for-timestamp=OFF",
	"--character-set-server=utf8",
	"--collation-server=utf8_general_ci",
}

type mysqlTestContainerConfig struct {
	image      string
	database   string
	password   string
	username   string
	useTls     bool
	serverArgs []string
}

// Holds the MySQL test container and connection pool.
type MysqlTestContainer struct {
	DB            *sql.DB
	URL           string
	TestContainer *testmysql.MySQLContainer

	cfg *mysqlTestContainerConfig
}

// Option is a functional option for configuring the Mysql Test Container
type Option func(*mysqlTestContainerConfig)

// NewMysqlTestContainer initializes a new MySQL Test Container with functional options
func NewMysqlTestContainer(ctx context.Context, opts ...Option) (*MysqlTestContainer, error) {
	m := &mysqlTestContainerConfig{
		image:    defaultMysqlImage,
		database: "testdb",
		username: "root",
		password: "pass",
		useTls:   false,
	}
	for _, opt := range opts {
		opt(m)
	}
	return setup(ctx, m)
}

// Sets test container database
func WithDatabase(database string) Option {
	return func(a *mysqlTestContainerConfig) {
		a.database = database
	}
}

// Sets test container database
func WithUsername(username string) Option {
	return func(a *mysqlTestContainerConfig) {
		a.username = username
	}
}

// Sets test container database
func WithPassword(password string) Option {
	return func(a *mysqlTestContainerConfig) {
		a.password = password
	}
}

func WithTls() Option {
	return func(mtc *mysqlTestContainerConfig) {
		mtc.useTls = true
	}
}

// Sets the container image (e.g. "mariadb:11.4.13"); defaults to defaultMysqlImage
func WithImage(image string) Option {
	return func(mtc *mysqlTestContainerConfig) {
		mtc.image = image
	}
}

// WithServerArgs passes options to the server (e.g. "--sql-mode="), after the ones the test
// container sets itself. They are added to those of earlier options.
func WithServerArgs(args ...string) Option {
	return func(mtc *mysqlTestContainerConfig) {
		mtc.serverArgs = append(mtc.serverArgs, args...)
	}
}

// WithMysql57 runs a MySQL 5.7 server (Mysql57Image) configured like the legacy production
// servers: empty sql_mode, explicit_defaults_for_timestamp=OFF and a utf8 server character set
// (see mysql57ServerArgs). Options given after it may override the image or add server
// options.
func WithMysql57() Option {
	return func(mtc *mysqlTestContainerConfig) {
		mtc.image = Mysql57Image
		mtc.serverArgs = append(mtc.serverArgs, mysql57ServerArgs...)
	}
}

// mysqlImageVersion reads the version a mysql image is tagged with: "mysql:8.4.11" gives 8
// and 4, "mysql:8" gives 8 and -1 (no minor version). ok is false for an image of another
// server (MariaDB) and for a tag that names no version (latest, lts, a digest).
func mysqlImageVersion(image string) (major, minor int, ok bool) {
	tag, found := strings.CutPrefix(image, "mysql:")
	if !found {
		return 0, 0, false
	}
	parts := strings.SplitN(tag, ".", 3)
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	minor = -1
	if len(parts) > 1 {
		// "8.0-debian" and the like: only the leading digits count.
		digits := strings.IndexFunc(parts[1], func(r rune) bool { return r < '0' || r > '9' })
		if digits == -1 {
			digits = len(parts[1])
		}
		if n, err := strconv.Atoi(parts[1][:digits]); err == nil {
			minor = n
		}
	}
	return major, minor, true
}

// knowsRestrictFkOnNonStandardKey tells whether the server of a mysql image has the option
// restrict_fk_on_non_standard_key, which appeared in 8.4.0: 5.7 and 8.0 refuse to start with
// it. A tag that names no version (latest, lts) or no minor version (mysql:8) is taken for a
// recent server.
func knowsRestrictFkOnNonStandardKey(image string) bool {
	if !strings.HasPrefix(image, "mysql:") {
		return false
	}
	major, minor, ok := mysqlImageVersion(image)
	if !ok || major > 8 {
		return true
	}
	return major == 8 && (minor == -1 || minor >= 4)
}

// tlsVersions gives the TLS versions the server is started with. The mysql:5.7 images are
// built on Oracle Linux 7 against OpenSSL 1.0.2: 5.7.42 takes TLSv1.3 in --tls-version but fails
// every TLSv1.3 handshake, so it is given TLSv1.2 alone.
func tlsVersions(image string) string {
	if IsMysql57(image) {
		return "TLSv1.2"
	}
	return "TLSv1.2,TLSv1.3"
}

// IsMysql57 tells whether the image is a MySQL 5.7 server.
func IsMysql57(image string) bool {
	major, minor, ok := mysqlImageVersion(image)
	return ok && major == 5 && minor == 7
}

// Creates and starts a MySQL test container and sets up the connection.
func setup(ctx context.Context, cfg *mysqlTestContainerConfig) (*MysqlTestContainer, error) {
	// the MariaDB entrypoint also accepts the MYSQL_* env vars set by the mysql module
	readyLog := mysqlReadyLog
	if strings.Contains(cfg.image, "mariadb") {
		readyLog = mariadbReadyLog
	}
	tcopts := []testcontainers.ContainerCustomizer{
		testmysql.WithDatabase(cfg.database),
		testmysql.WithUsername(cfg.username),
		testmysql.WithPassword(cfg.password),
		testcontainers.WithWaitStrategy(
			wait.ForLog(readyLog).
				WithOccurrence(1).
				// Generous timeout: the workflow integration test starts several
				// DB containers in parallel (source+target for mysql, mariadb and
				// mssql), and MariaDB's init phase (temp server -> init SQL ->
				// real server) can exceed a tight deadline under CI contention.
				WithStartupTimeout(120 * time.Second),
		),
	}
	serverArgs := []string{}
	if knowsRestrictFkOnNonStandardKey(cfg.image) {
		// MySQL 8.4 refuses to create a foreign key on a non-unique or partial
		// key, which 8.0 allowed and keeps across an upgrade. The fixtures
		// model such source databases, so the test server accepts them too.
		// The option does not exist before 8.4, where such keys are allowed.
		serverArgs = append(serverArgs, "--restrict-fk-on-non-standard-key=OFF")
	}
	if cfg.useTls {
		clientCertPaths, err := testutil.GetTlsCertificatePaths()
		if err != nil {
			return nil, err
		}
		serverArgs = append(
			serverArgs,
			"--ssl-ca=/etc/mysql/certs/root.crt",
			"--ssl-cert=/etc/mysql/certs/server.crt",
			"--ssl-key=/etc/mysql/certs/server.key",
			"--require-secure-transport=ON",
			"--tls-version="+tlsVersions(cfg.image),
		)
		tcopts = append(
			tcopts,
			testutil.WithFiles([]testcontainers.ContainerFile{
				{
					HostFilePath:      clientCertPaths.ServerCertPath,
					ContainerFilePath: "/etc/mysql/certs/server.crt",
					FileMode:          0644,
				},
				{
					HostFilePath:      clientCertPaths.ServerKeyPath,
					ContainerFilePath: "/etc/mysql/certs/server.key",
					FileMode:          0600,
				},
				{
					HostFilePath:      clientCertPaths.RootCertPath,
					ContainerFilePath: "/etc/mysql/certs/root.crt",
					FileMode:          0644,
				},
			}),
			testcontainers.WithStartupCommand(testcontainers.NewRawCommand([]string{
				"chown", "mysql:mysql", "/etc/mysql/certs/server.key",
			})),
		)
	}
	serverArgs = append(serverArgs, cfg.serverArgs...)
	if len(serverArgs) > 0 {
		tcopts = append(tcopts, testutil.WithCmd(append([]string{"mysqld"}, serverArgs...)))
	}
	mysqlContainer, err := testmysql.Run(
		ctx,
		cfg.image,
		tcopts...,
	)
	if err != nil {
		return nil, err
	}

	connstrArgs := []string{"multiStatements=true", "parseTime=true"}
	if cfg.useTls {
		connstrArgs = append(connstrArgs, "tls=true")
	}

	connStr, err := mysqlContainer.ConnectionString(ctx, connstrArgs...)
	if err != nil {
		return nil, err
	}

	connectorOpts := []mysqltunconnector.Option{}
	if cfg.useTls {
		serverHost, err := mysqlContainer.Host(ctx)
		if err != nil {
			return nil, err
		}
		tlsConfig, err := clientTlsConfig(cfg, serverHost)
		if err != nil {
			return nil, err
		}
		connectorOpts = append(connectorOpts, mysqltunconnector.WithTLSConfig(tlsConfig))
	}

	connector, _, err := mysqltunconnector.New(connStr, connectorOpts...)
	if err != nil {
		return nil, err
	}

	db := sql.OpenDB(connector)

	return &MysqlTestContainer{
		DB:            db,
		URL:           connStr,
		TestContainer: mysqlContainer,

		cfg: cfg,
	}, nil
}

// Image is the image the server runs, e.g. Mysql57Image.
func (m *MysqlTestContainer) Image() string {
	return m.cfg.image
}

// IsMysql57 tells whether the server is a MySQL 5.7 one.
func (m *MysqlTestContainer) IsMysql57() bool {
	return IsMysql57(m.cfg.image)
}

func (m *MysqlTestContainer) GetClientTlsConfig(ctx context.Context) (*tls.Config, error) {
	if !m.cfg.useTls {
		return nil, errors.New("tls is not enabled on this test container")
	}

	serverHost, err := m.TestContainer.Host(ctx)
	if err != nil {
		return nil, err
	}

	return clientTlsConfig(m.cfg, serverHost)
}

// clientTlsConfig gives the TLS configuration a client of the server connects with. The OpenSSL
// 1.0.2 of the mysql:5.7 images negotiates no ECDHE suite (no curve is set up) and offers, besides,
// only DHE suites, which Go does not implement, and suites with an RSA key exchange, which Go
// leaves out of its defaults: the client of a 5.7 server asks for the latter by name.
func clientTlsConfig(cfg *mysqlTestContainerConfig, serverHost string) (*tls.Config, error) {
	tlsConfig, err := testutil.GetClientTlsConfig(serverHost)
	if err != nil {
		return nil, err
	}
	if IsMysql57(cfg.image) {
		tlsConfig.MaxVersion = tls.VersionTLS12
		tlsConfig.CipherSuites = []uint16{
			tls.TLS_RSA_WITH_AES_128_GCM_SHA256, //nolint:gosec // the only suites the 5.7 image and Go share
			tls.TLS_RSA_WITH_AES_256_GCM_SHA384, //nolint:gosec // the only suites the 5.7 image and Go share
		}
	}
	return tlsConfig, nil
}

// Closes the connection pool and terminates the container.
func (m *MysqlTestContainer) TearDown(ctx context.Context) error {
	if m.DB != nil {
		m.DB.Close()
	}

	if m.TestContainer != nil {
		err := m.TestContainer.Terminate(ctx)
		if err != nil {
			return fmt.Errorf("failed to terminate MySQL container: %w", err)
		}
	}

	return nil
}

// Executes SQL files within the test container
func (m *MysqlTestContainer) RunSqlFiles(
	ctx context.Context,
	folder *string,
	files []string,
) error {
	for _, file := range files {
		filePath := file
		if folder != nil && *folder != "" {
			filePath = fmt.Sprintf("./%s/%s", *folder, file)
		}
		sqlStr, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}
		_, err = m.DB.ExecContext(ctx, string(sqlStr))
		if err != nil {
			return fmt.Errorf("unable to exec SQL when running MySQL SQL files: %w", err)
		}
	}
	return nil
}

// Creates schema and sets USE to schema before running SQL files
func (m *MysqlTestContainer) RunCreateStmtsInDatabase(
	ctx context.Context,
	folder string,
	files []string,
	database string,
) error {
	for _, file := range files {
		filePath := file
		if folder != "" {
			filePath = fmt.Sprintf("./%s/%s", folder, file)
		}
		sqlStr, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}

		setSchemaSql := fmt.Sprintf(
			"CREATE DATABASE IF NOT EXISTS `%s`; \n USE `%s`; \n",
			database,
			database,
		)
		_, err = m.DB.ExecContext(ctx, setSchemaSql+string(sqlStr))
		if err != nil {
			return fmt.Errorf("unable to exec sql when running mysql sql files: %w", err)
		}
	}
	return nil
}

func (m *MysqlTestContainer) CreateDatabases(ctx context.Context, databases []string) error {
	for _, database := range databases {
		_, err := m.DB.ExecContext(
			ctx,
			fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`;", database),
		)
		if err != nil {
			return fmt.Errorf("unable to create database %s: %w", database, err)
		}
	}
	return nil
}

func (m *MysqlTestContainer) DropDatabases(ctx context.Context, databases []string) error {
	for _, database := range databases {
		_, err := m.DB.ExecContext(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS `%s`;", database))
		if err != nil {
			return fmt.Errorf("unable to drop database %s: %w", database, err)
		}
	}
	return nil
}

func (m *MysqlTestContainer) GetTableRowCount(
	ctx context.Context,
	schema, table string,
) (int, error) {
	rows := m.DB.QueryRowContext(
		ctx,
		fmt.Sprintf("SELECT COUNT(*) FROM  `%s`.`%s`;", schema, table),
	)
	var count int
	err := rows.Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}
