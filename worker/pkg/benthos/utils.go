package husonym_benthos

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
)

func BuildBenthosTable(schema, table string) string {
	if schema != "" {
		return fmt.Sprintf("%s.%s", schema, table)
	}
	return table
}

func HashBenthosCacheKey(jobId, runId, table, col string) string {
	return ToSha256(fmt.Sprintf("%s.%s.%s.%s", jobId, runId, table, col))
}

func ToSha256(input string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(input)))
}

// checks if the error message is critical
func IsCriticalError(errMsg string) bool {
	// list of known error messages for when max connections are reached
	criticalErrors := []string{
		"violates foreign key constraint",
		"duplicate key value violates unique constraint",
		"duplicate entry",
		"cannot add or update a child row",
		"a foreign key constraint fails",
		"could not identify an equality operator",
		"violates not-null constraint",
		"failed to send message to redis_hash_output",
		"mapping returned invalid key type",
		"invalid input syntax",
		"incorrect datetime value",
		"incorrect date value",
		"incorrect time value",
		"does not exist",
		"syntax error at or near",
		"ON CONFLICT DO UPDATE requires inference specification or constraint name",
		"transaction has already been committed or rolled back",
		"missing redis client",
		"violates check constraint",
		"ON CONFLICT does not support deferrable unique constraints",
		"ON CONFLICT",
		"SQLSTATE", // any sqlstate error should result in ending
		"goqu_encode_error",
		"doesn't have a default value",
		"column does not allow nulls",
	}

	for _, errStr := range criticalErrors {
		if containsIgnoreCase(errMsg, errStr) {
			return true
		}
	}
	return isPermanentMysqlServerError(errMsg)
}

// mysqlServerError matches an error returned by a MySQL server: "Error 1264 (22003): …".
// Driver-side failures (lost connection, bad connection) have no such prefix.
var mysqlServerError = regexp.MustCompile(`Error (\d+) \([0-9A-Z]{5}\)`)

// mysqlTransientErrors are the server errors worth another try: the same statement can
// succeed once the contention or the connection shortage is over.
var mysqlTransientErrors = map[string]bool{
	"1040": true, // too many connections
	"1203": true, // too many connections for this user
	"1205": true, // lock wait timeout
	"1213": true, // deadlock
}

// isPermanentMysqlServerError reports a MySQL server error that the same statement will
// get again: value out of range, data too long, generated column, command denied, unknown
// or ambiguous column… Listing such messages one by one left most of them out, and the
// stream retried them until the activity timed out, ten minutes later, without ever
// failing the run.
func isPermanentMysqlServerError(errMsg string) bool {
	match := mysqlServerError.FindStringSubmatch(errMsg)
	return len(match) == 2 && !mysqlTransientErrors[match[1]]
}

// checks if the error message is critical for the generate job
func IsGenerateJobCriticalError(errMsg string) bool {
	criticalErrors := []string{
		"violates foreign key constraint",
		"cannot add or update a child row",
		"a foreign key constraint fails",
		"could not identify an equality operator",
		"violates not-null constraint",
		"invalid input syntax",
		"incorrect datetime value",
		"incorrect date value",
		"incorrect time value",
		"does not exist",
		"syntax error at or near",
		"doesn't have a default value",
		"column does not allow nulls",
	}

	for _, errStr := range criticalErrors {
		if containsIgnoreCase(errMsg, errStr) {
			return true
		}
	}
	return false
}

func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

func IsForeignKeyViolationError(errMsg string) bool {
	foreignKeyViolationErrors := []string{
		"violates foreign key constraint",
		"a foreign key constraint fails",
		"insert statement conflicted with the foreign key constraint",
	}

	for _, errStr := range foreignKeyViolationErrors {
		if containsIgnoreCase(errMsg, errStr) {
			return true
		}
	}
	return false
}

func ShouldRetryInsert(errMsg string, shouldCheckForForeignKeyViolation bool) bool {
	if shouldCheckForForeignKeyViolation && IsForeignKeyViolationError(errMsg) {
		return true
	}
	otherErrors := []string{
		"ON CONFLICT DO UPDATE command cannot affect row a second time",
	}
	for _, errStr := range otherErrors {
		if containsIgnoreCase(errMsg, errStr) {
			return true
		}
	}
	return false
}
