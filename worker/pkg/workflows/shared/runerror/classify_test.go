package runerror

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	connectionRefused      = mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CONNECTION_REFUSED
	authenticationRefused  = mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_AUTHENTICATION_REFUSED
	timeout                = mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_TIMEOUT
	constraintViolated     = mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CONSTRAINT_VIOLATED
	insufficientPrivileges = mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_INSUFFICIENT_PRIVILEGES
	objectMissing          = mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_OBJECT_MISSING
	typeMismatch           = mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_TYPE_MISMATCH
	resourcesExhausted     = mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_RESOURCES_EXHAUSTED
	canceled               = mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_CANCELED
	license                = mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_LICENSE
	other                  = mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_OTHER
	unspecified            = mgmtv1alpha1.RunErrorCategory_RUN_ERROR_CATEGORY_UNSPECIFIED
)

func Test_PostgresCategory(t *testing.T) {
	cases := map[string]mgmtv1alpha1.RunErrorCategory{
		// the exact codes
		"57014": timeout,
		"55P03": timeout,
		"25P03": timeout,
		"57P05": timeout,
		"57P01": connectionRefused,
		"57P02": connectionRefused,
		"57P03": connectionRefused,
		"42501": insufficientPrivileges,
		"42P01": objectMissing,
		"42703": objectMissing,
		"42704": objectMissing,
		"42883": objectMissing,
		"3D000": objectMissing,
		"3F000": objectMissing,
		"42804": typeMismatch,
		"42846": typeMismatch,
		// the classes
		"08000": connectionRefused,
		"08006": connectionRefused,
		"08P01": connectionRefused,
		"28000": authenticationRefused,
		"28P01": authenticationRefused,
		"23000": constraintViolated,
		"23502": constraintViolated,
		"23503": constraintViolated,
		"23505": constraintViolated,
		"23514": constraintViolated,
		"22000": typeMismatch,
		"22001": typeMismatch,
		"22003": typeMismatch,
		"22P02": typeMismatch,
		"53000": resourcesExhausted,
		"53100": resourcesExhausted,
		"53200": resourcesExhausted,
		"53300": resourcesExhausted,
		// an exact code wins over nothing: its class is not listed
		"57000": other,
		"57P04": other,
		"55006": other,
		"25000": other,
		"3D001": other,
		// what is not listed
		"42601":  other,
		"42000":  other,
		"40001":  other,
		"40P01":  other,
		"XX000":  other,
		"00000":  other,
		"":       other,
		"4":      other,
		"2":      other,
		"23":     other,
		"230000": other,
	}
	for sqlstate, expected := range cases {
		t.Run(sqlstate, func(t *testing.T) {
			assert.Equal(t, expected, postgresCategory(sqlstate))
		})
	}
}

func Test_MySQLCategory(t *testing.T) {
	cases := map[uint16]mgmtv1alpha1.RunErrorCategory{
		1045:  authenticationRefused,
		1044:  insufficientPrivileges,
		1062:  constraintViolated,
		1146:  objectMissing,
		1366:  typeMismatch,
		1205:  timeout,
		1114:  resourcesExhausted,
		1130:  connectionRefused,
		1064:  other,
		1213:  other,
		0:     other,
		2002:  other,
		65535: other,
	}
	for number, expected := range cases {
		t.Run(strconv.Itoa(int(number)), func(t *testing.T) {
			assert.Equal(t, expected, mysqlCategory(number))
		})
	}
}

// The two tables, as the plan of the classification lists them: every code has a category of
// its own, and the one written here.
func Test_EveryListedCodeHasItsCategory(t *testing.T) {
	postgres := map[mgmtv1alpha1.RunErrorCategory][]string{
		timeout:                {"57014", "55P03", "25P03", "57P05"},
		connectionRefused:      {"57P01", "57P02", "57P03"},
		insufficientPrivileges: {"42501"},
		objectMissing:          {"42P01", "42703", "42704", "42883", "3D000", "3F000"},
		typeMismatch:           {"42804", "42846"},
	}
	listed := 0
	for expected, codes := range postgres {
		for _, code := range codes {
			listed++
			assert.Equal(t, expected, postgresCategory(code), "SQLSTATE %s", code)
			assert.NotEqual(t, other, postgresCategory(code), "SQLSTATE %s", code)
		}
	}
	assert.Len(t, postgresCodes, listed, "a code is in the table that this test does not know")

	classes := map[string]mgmtv1alpha1.RunErrorCategory{
		"08": connectionRefused,
		"28": authenticationRefused,
		"23": constraintViolated,
		"22": typeMismatch,
		"53": resourcesExhausted,
	}
	for class, expected := range classes {
		assert.Equal(t, expected, postgresCategory(class+"ZZZ"), "class %s", class)
	}
	assert.Len(t, postgresClasses, len(classes), "a class is in the table that this test does not know")

	numbersOfMySQL := map[mgmtv1alpha1.RunErrorCategory][]uint16{
		connectionRefused:      {1053, 1129, 1130},
		authenticationRefused:  {1045, 1698, 1862, 3118},
		insufficientPrivileges: {1044, 1142, 1143, 1227, 1370},
		timeout:                {1205, 3024},
		constraintViolated:     {1048, 1062, 1169, 1216, 1217, 1364, 1451, 1452, 1586, 3819},
		objectMissing:          {1049, 1051, 1054, 1146, 1305},
		typeMismatch:           {1264, 1265, 1292, 1366, 1406, 3140},
		resourcesExhausted:     {1021, 1037, 1038, 1040, 1041, 1114, 1203, 1226},
	}
	listed = 0
	for expected, numbers := range numbersOfMySQL {
		for _, number := range numbers {
			listed++
			assert.Equal(t, expected, mysqlCategory(number), "MySQL error %d", number)
			assert.NotEqual(t, other, mysqlCategory(number), "MySQL error %d", number)
		}
	}
	assert.Len(t, mysqlNumbers, listed, "a number is in the table that this test does not know")
}

// timeoutError is a network error that says it timed out.
type timeoutError struct{ timedOut bool }

func (e *timeoutError) Error() string   { return "i/o" }
func (e *timeoutError) Timeout() bool   { return e.timedOut }
func (e *timeoutError) Temporary() bool { return false }

func Test_Classify(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		expected mgmtv1alpha1.RunErrorCategory
	}{
		{"a constraint of PostgreSQL", &pgconn.PgError{Code: "23505"}, constraintViolated},
		{"a statement PostgreSQL canceled for its duration", &pgconn.PgError{Code: "57014"}, timeout},
		{"a value PostgreSQL cannot read", &pgconn.PgError{Code: "22P02"}, typeMismatch},
		{"a code of PostgreSQL that is not listed", &pgconn.PgError{Code: "42601"}, other},
		{"a refused login of MySQL", &mysql.MySQLError{Number: 1045}, authenticationRefused},
		{"a number of MySQL that is not listed", &mysql.MySQLError{Number: 1064}, other},
		{"a deadline", context.DeadlineExceeded, timeout},
		{"a deadline of a file or a socket", os.ErrDeadlineExceeded, timeout},
		{"a cancellation", context.Canceled, canceled},
		{
			"a statement canceled because the run was",
			errors.Join(&pgconn.PgError{Code: "57014"}, context.Canceled),
			canceled,
		},
		{
			"a deadline that ended a statement",
			fmt.Errorf("%w: %w", context.DeadlineExceeded, &pgconn.PgError{Code: "23505"}),
			timeout,
		},
		{
			"a statement that failed under a deadline",
			fmt.Errorf("%w: %w", &pgconn.PgError{Code: "23505"}, context.DeadlineExceeded),
			timeout,
		},
		{"a refused dial", &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}, connectionRefused},
		{"a network error that timed out", &timeoutError{timedOut: true}, timeout},
		{"a dial that timed out", &net.OpError{Op: "dial", Err: &timeoutError{timedOut: true}}, timeout},
		{"a network error that did not time out", &timeoutError{}, other},
		{"a name that does not resolve", &net.DNSError{}, connectionRefused},
		{"a name that took too long to resolve", &net.DNSError{IsTimeout: true}, timeout},
		{"a connection PostgreSQL did not open", &pgconn.ConnectError{}, connectionRefused},
		{
			"a login PostgreSQL refused when connecting",
			pgConnectError(&pgconn.PgError{Code: "28P01"}),
			authenticationRefused,
		},
		{"a broken connection of MySQL", mysql.ErrInvalidConn, connectionRefused},
		{"a broken connection of a driver", driver.ErrBadConn, connectionRefused},
		{"a full disk", syscall.ENOSPC, resourcesExhausted},
		{"no memory left", syscall.ENOMEM, resourcesExhausted},
		{"a full disk under a file operation", &os.PathError{Op: "write", Err: syscall.ENOSPC}, resourcesExhausted},
		{"another error of the system", syscall.EACCES, other},
		{"a refusal of the license", License(errors.New("x")), license},
		{"a refusal of the license over a database error", License(&pgconn.PgError{Code: "23505"}), license},
		{
			"an error that only says so",
			errors.New("connection refused: permission denied for table users (SQLSTATE 42501)"),
			other,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, Classify(tc.err), "bare")
			wrapped := fmt.Errorf("syncing: %w", fmt.Errorf("writing users: %w", tc.err))
			assert.Equal(t, tc.expected, Classify(wrapped), "wrapped twice")
			deep := tc.err
			for range 12 {
				deep = fmt.Errorf("deeper: %w", deep)
			}
			assert.Equal(t, tc.expected, Classify(errors.Join(errors.New("first"), deep)), "deep under a join")
		})
	}

	assert.Equal(t, other, Classify(nil))
	for _, tc := range cases {
		assert.NotEqual(t, unspecified, Classify(tc.err), tc.name)
	}
}

// pgConnectError is a pgconn.ConnectError over a cause. Its cause is not exported: the driver
// alone builds one, and tells its cause through Unwrap.
func pgConnectError(cause error) error {
	return &wrapping{outer: &pgconn.ConnectError{}, cause: cause}
}

// wrapping gives the errors of a join, without being one.
type wrapping struct {
	outer error
	cause error
}

func (w *wrapping) Error() string   { return "wrapping" }
func (w *wrapping) Unwrap() []error { return []error{w.outer, w.cause} }

// mute is an error that cannot be asked for its text.
type mute struct{ cause error }

func (m *mute) Error() string { panic("the text of an error was read") }
func (m *mute) Unwrap() error { return m.cause }

func Test_Classify_NeverReadsTheMessage(t *testing.T) {
	t.Run("the message of the database does not decide", func(t *testing.T) {
		err := &mute{cause: &pgconn.PgError{Code: "42501", Message: "duplicate key"}}
		require.NotPanics(t, func() {
			assert.Equal(t, insufficientPrivileges, Classify(err))
		})
		assert.Equal(t, other, Classify(&pgconn.PgError{Message: "permission denied", Detail: "23505"}))
		assert.Equal(t, other, Classify(&mysql.MySQLError{Message: "Access denied for user"}))
	})

	t.Run("no error is asked for its text, whatever its category", func(t *testing.T) {
		causes := []error{
			&pgconn.PgError{Code: "23505"}, &mysql.MySQLError{Number: 1045}, context.DeadlineExceeded,
			context.Canceled, &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}, &net.DNSError{},
			mysql.ErrInvalidConn, driver.ErrBadConn, syscall.ENOSPC, License(errors.New("x")),
			errors.New("x"),
		}
		for _, cause := range causes {
			require.NotPanics(t, func() {
				assert.Equal(t, Classify(cause), Classify(&mute{cause: cause}))
				assert.Equal(t, CategoryOf(cause), CategoryOf(&mute{cause: cause}))
				_, _ = Carried(&mute{cause: cause})
			})
		}
		require.NotPanics(t, func() { _ = License(&mute{}) })
	})
}

// The guard of the rule "never the text", on the source itself: nothing of the package asks
// an error for its text, and nothing of it can search a text. The one text the package
// handles is the message Temporal computed for the failure, which Carry hands back to
// Temporal untouched (carry.go).
func Test_ThePackage_NeverAsksAnErrorForItsText(t *testing.T) {
	files, err := filepath.Glob("*.go")
	require.NoError(t, err)

	// What reads, formats or searches a text.
	forbiddenImports := map[string]bool{
		"fmt": true, "strings": true, "regexp": true, "bytes": true, "log": true, "log/slog": true,
	}
	// The methods and members that give the text of an error, of a failure or of a database
	// error.
	forbiddenSelectors := map[string]bool{
		"Error": true, "Message": true, "String": true, "Detail": true, "Hint": true,
		"Where": true, "GoString": true, "StackTrace": true,
	}

	sources := 0
	messageReads := []string{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		sources++
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		require.NoError(t, err)

		packages := map[string]bool{}
		for _, imported := range parsed.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			require.NoError(t, err)
			assert.False(t, forbiddenImports[path], "%s imports %s, which handles text", file, path)
			name := filepath.Base(path)
			if imported.Name != nil {
				name = imported.Name.Name
			}
			packages[name] = true
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			// A name of a package, as the type net.Error, is not a member of an error.
			if qualifier, ok := selector.X.(*ast.Ident); ok && packages[qualifier.Name] {
				return true
			}
			position := fset.Position(selector.Sel.Pos()).String()
			assert.False(t, forbiddenSelectors[selector.Sel.Name],
				"%s reads .%s: the text of an error decides nothing here", position, selector.Sel.Name)
			if selector.Sel.Name == "GetMessage" {
				messageReads = append(messageReads, filepath.Base(fset.Position(selector.Sel.Pos()).Filename))
			}
			return true
		})
	}
	require.Positive(t, sources, "no source was read")
	assert.Equal(t, []string{"carry.go"}, messageReads,
		"the message of a failure is read once, by Carry, to give it back to Temporal as it is")
}
