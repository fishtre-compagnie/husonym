package sqlmanager_shared

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_BuildForeignKeyConstraintFingerprint(t *testing.T) {
	updateRule := "CASCADE"
	deleteRule := "NO ACTION"

	fk := &ForeignKeyConstraint{
		ReferencingSchema:  "public",
		ReferencingTable:   "users",
		ReferencingColumns: []string{"role_id", "team_id"},
		ReferencedSchema:   "public",
		ReferencedTable:    "roles",
		ReferencedColumns:  []string{"id", "team_id"},
		ConstraintType:     "FOREIGN KEY",
		NotNullable:        []bool{true, false},
		UpdateRule:         &updateRule,
		DeleteRule:         &deleteRule,
	}

	result := BuildForeignKeyConstraintFingerprint(fk)
	require.NotEmpty(t, result)

	// Test that order of columns doesn't affect fingerprint
	fk2 := &ForeignKeyConstraint{
		ReferencingSchema:  "public",
		ReferencingTable:   "users",
		ReferencingColumns: []string{"team_id", "role_id"},
		ReferencedSchema:   "public",
		ReferencedTable:    "roles",
		ReferencedColumns:  []string{"team_id", "id"},
		ConstraintType:     "FOREIGN KEY",
		NotNullable:        []bool{true, false},
		UpdateRule:         &updateRule,
		DeleteRule:         &deleteRule,
	}

	result2 := BuildForeignKeyConstraintFingerprint(fk2)
	require.Equal(t, result, result2)
}

func Test_BuildNonForeignKeyConstraintFingerprint(t *testing.T) {
	nfk := &NonForeignKeyConstraint{
		SchemaName:     "public",
		TableName:      "users",
		ConstraintType: "PRIMARY KEY",
		Columns:        []string{"id", "email"},
		Definition:     "",
	}

	result := BuildNonForeignKeyConstraintFingerprint(nfk)
	require.NotEmpty(t, result)

	// Test that order of columns doesn't affect fingerprint
	nfk2 := &NonForeignKeyConstraint{
		SchemaName:     "public",
		TableName:      "users",
		ConstraintType: "PRIMARY KEY",
		Columns:        []string{"email", "id"},
		Definition:     "",
	}

	result2 := BuildNonForeignKeyConstraintFingerprint(nfk2)
	require.Equal(t, result, result2)
}

func Test_BuildNonForeignKeyConstraintFingerprint_ColumnPrefixes(t *testing.T) {
	withoutPrefix := &NonForeignKeyConstraint{
		SchemaName:     "public",
		TableName:      "users",
		ConstraintType: "UNIQUE",
		Columns:        []string{"endpoint"},
	}

	t.Run("a constraint without prefixes keeps its former fingerprint", func(t *testing.T) {
		// Pinned against the parts the fingerprint was built from before prefixes
		// existed: nothing that carries no prefix may churn through the schema diff.
		require.Equal(
			t,
			BuildFingerprint("UNIQUE", "public", "users", "endpoint", ""),
			BuildNonForeignKeyConstraintFingerprint(withoutPrefix),
		)
	})

	withPrefix := &NonForeignKeyConstraint{
		SchemaName:     "public",
		TableName:      "users",
		ConstraintType: "UNIQUE",
		Columns:        []string{"endpoint"},
		ColumnPrefixes: map[string]int64{"endpoint": 255},
	}

	t.Run("a prefix makes the constraint differ from the same one without", func(t *testing.T) {
		require.NotEqual(
			t,
			BuildNonForeignKeyConstraintFingerprint(withoutPrefix),
			BuildNonForeignKeyConstraintFingerprint(withPrefix),
		)
	})

	t.Run("prefix lengths are compared, not just their presence", func(t *testing.T) {
		shorter := *withPrefix
		shorter.ColumnPrefixes = map[string]int64{"endpoint": 100}
		require.NotEqual(
			t,
			BuildNonForeignKeyConstraintFingerprint(withPrefix),
			BuildNonForeignKeyConstraintFingerprint(&shorter),
		)
	})

	t.Run("map iteration order does not move the fingerprint", func(t *testing.T) {
		composite := &NonForeignKeyConstraint{
			SchemaName:     "public",
			TableName:      "users",
			ConstraintType: "UNIQUE",
			Columns:        []string{"a", "b", "c"},
			ColumnPrefixes: map[string]int64{"a": 10, "b": 20, "c": 30},
		}
		expected := BuildNonForeignKeyConstraintFingerprint(composite)
		for range 20 {
			require.Equal(t, expected, BuildNonForeignKeyConstraintFingerprint(composite))
		}
	})
}

func Test_BoolSliceToString(t *testing.T) {
	result := boolSliceToString([]bool{true, false, true})
	require.Equal(t, "1,0,1", result)

	result = boolSliceToString([]bool{})
	require.Equal(t, "", result)
}

func Test_PtrOrEmpty(t *testing.T) {
	str := "test"
	result := ptrOrEmpty(&str)
	require.Equal(t, "test", result)

	result = ptrOrEmpty(nil)
	require.Equal(t, "", result)
}

func Test_BuildFingerprint(t *testing.T) {
	result := BuildFingerprint("test")
	require.Equal(t, "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08", result)
}

func Test_BuildTriggerFingerprint(t *testing.T) {
	// Test basic trigger fingerprint
	trigger := &TableTrigger{
		Schema:      "public",
		Table:       "users",
		TriggerName: "users_audit",
		Definition:  "BEGIN INSERT INTO audit_log VALUES (NEW.*); END;",
	}

	result := BuildTriggerFingerprint(trigger)
	require.NotEmpty(t, result)

	// Test that same trigger details produce same fingerprint
	trigger2 := &TableTrigger{
		Schema:      "public",
		Table:       "users",
		TriggerName: "users_audit",
		Definition:  "BEGIN INSERT INTO audit_log VALUES (NEW.*); END;",
	}

	result2 := BuildTriggerFingerprint(trigger2)
	require.Equal(t, result, result2)

	// Test with trigger schema
	triggerSchema := "triggers"
	trigger3 := &TableTrigger{
		Schema:        "public",
		Table:         "users",
		TriggerName:   "users_audit",
		Definition:    "BEGIN INSERT INTO audit_log VALUES (NEW.*); END;",
		TriggerSchema: &triggerSchema,
	}

	result3 := BuildTriggerFingerprint(trigger3)
	require.NotEqual(t, result, result3)

	// Test that empty trigger schema is handled
	emptySchema := ""
	trigger4 := &TableTrigger{
		Schema:        "public",
		Table:         "users",
		TriggerName:   "users_audit",
		Definition:    "BEGIN INSERT INTO audit_log VALUES (NEW.*); END;",
		TriggerSchema: &emptySchema,
	}

	result4 := BuildTriggerFingerprint(trigger4)
	require.Equal(t, result, result4)
}
