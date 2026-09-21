package v1alpha1_connectiondataservice

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	javascript_draft "github.com/fishtre-compagnie/husonym/internal/javascript/draft"
	"github.com/stretchr/testify/require"
)

func Test_isNullable(t *testing.T) {
	require.True(t, isNullable("YES"))
	require.True(t, isNullable("yes"))
	require.True(t, isNullable("true"))
	require.False(t, isNullable("NO"))
	require.False(t, isNullable("false"))
	// An unknown flag must not read as nullable: a rule allowed to return null into a NOT NULL
	// column fails at write time, after the run has started.
	require.False(t, isNullable(""))
	require.False(t, isNullable("unknown"))
}

func Test_maxLengthOf(t *testing.T) {
	column := func(dataType string, length *int32) *mgmtv1alpha1.DatabaseColumn {
		return &mgmtv1alpha1.DatabaseColumn{
			DataType:               dataType,
			CharacterMaximumLength: length,
		}
	}
	length := func(v int32) *int32 { return &v }

	t.Run("reports the bound the schema carries", func(t *testing.T) {
		require.Equal(t, int32(20), *maxLengthOf(column("character varying", length(20))))
	})

	t.Run("mysql reports varchar with no length in the type", func(t *testing.T) {
		// The regression this replaced: MySQL puts `varchar` in data_type and keeps the length
		// in column_type, so parsing data_type found no bound and every MySQL column was drafted
		// as unbounded. The bound now travels in its own field, and the type string is irrelevant.
		require.Equal(t, int32(20), *maxLengthOf(column("varchar", length(20))))
	})

	t.Run("an unbounded type bounds nothing", func(t *testing.T) {
		require.Nil(t, maxLengthOf(column("text", nil)))
		require.Nil(t, maxLengthOf(column("integer", nil)))
	})

	t.Run("a non positive bound is no bound", func(t *testing.T) {
		// The drivers write -1 for "no bound"; the conversion already drops it, and this keeps a
		// stray value from reaching a rule as "at most -1 characters".
		require.Nil(t, maxLengthOf(column("varchar", length(-1))))
		require.Nil(t, maxLengthOf(column("varchar", length(0))))
	})
}

func Test_isUniqueAlone(t *testing.T) {
	constraints := &mgmtv1alpha1.GetConnectionTableConstraintsResponse{
		PrimaryKeyConstraints: map[string]*mgmtv1alpha1.PrimaryConstraint{
			"public.users": {Columns: []string{"id"}},
			"public.memberships": {
				Columns: []string{"user_id", "group_id"},
			},
		},
		UniqueConstraints: map[string]*mgmtv1alpha1.UniqueConstraints{
			"public.users": {Constraints: []*mgmtv1alpha1.UniqueConstraint{
				{Columns: []string{"login"}},
				{Columns: []string{"tenant_id", "reference"}},
			}},
		},
		UniqueIndexes: map[string]*mgmtv1alpha1.UniqueIndexes{
			"public.users": {Indexes: []*mgmtv1alpha1.UniqueIndex{
				{Columns: []string{"email"}},
			}},
		},
	}

	t.Run("a key over the column alone constrains it", func(t *testing.T) {
		require.True(t, isUniqueAlone(constraints, "public.users", "id"))
		require.True(t, isUniqueAlone(constraints, "public.users", "login"))
		require.True(t, isUniqueAlone(constraints, "public.users", "email"))
	})

	t.Run("a composite key does not", func(t *testing.T) {
		// The combination is unique, the column is not: claiming otherwise would forbid the
		// deterministic functions that are usually the right answer here.
		require.False(t, isUniqueAlone(constraints, "public.memberships", "user_id"))
		require.False(t, isUniqueAlone(constraints, "public.users", "tenant_id"))
	})

	t.Run("an unconstrained column is free", func(t *testing.T) {
		require.False(t, isUniqueAlone(constraints, "public.users", "first_name"))
		require.False(t, isUniqueAlone(constraints, "public.unknown", "id"))
	})
}

func Test_foreignKeyOf(t *testing.T) {
	constraints := &mgmtv1alpha1.GetConnectionTableConstraintsResponse{
		ForeignKeyConstraints: map[string]*mgmtv1alpha1.ForeignConstraintTables{
			"public.orders": {Constraints: []*mgmtv1alpha1.ForeignConstraint{
				{
					Columns: []string{"customer_id"},
					ForeignKey: &mgmtv1alpha1.ForeignKey{
						Table:   "public.customers",
						Columns: []string{"id"},
					},
				},
				{
					Columns: []string{"tenant_id", "site_code"},
					ForeignKey: &mgmtv1alpha1.ForeignKey{
						Table:   "sales.sites",
						Columns: []string{"tenant", "code"},
					},
				},
			}},
		},
	}

	t.Run("finds the parent of a simple key", func(t *testing.T) {
		require.Equal(t, &javascript_draft.ForeignKey{
			Schema: "public", Table: "customers", Column: "id",
		}, foreignKeyOf(constraints, "public.orders", "customer_id"))
	})

	t.Run("lines a composite key up by position", func(t *testing.T) {
		// site_code is second in the constraint, so its parent is the second parent column.
		// Taking the first would send the model at the wrong column, and the draft would look
		// right while breaking the key.
		require.Equal(t, &javascript_draft.ForeignKey{
			Schema: "sales", Table: "sites", Column: "code",
		}, foreignKeyOf(constraints, "public.orders", "site_code"))
	})

	t.Run("a column with no parent has none", func(t *testing.T) {
		require.Nil(t, foreignKeyOf(constraints, "public.orders", "total"))
		require.Nil(t, foreignKeyOf(constraints, "public.unknown", "id"))
	})
}

func Test_countReferencesTo(t *testing.T) {
	constraints := &mgmtv1alpha1.GetConnectionTableConstraintsResponse{
		ForeignKeyConstraints: map[string]*mgmtv1alpha1.ForeignConstraintTables{
			"public.orders": {Constraints: []*mgmtv1alpha1.ForeignConstraint{
				{
					Columns:    []string{"customer_id"},
					ForeignKey: &mgmtv1alpha1.ForeignKey{Table: "public.customers", Columns: []string{"id"}},
				},
			}},
			"public.invoices": {Constraints: []*mgmtv1alpha1.ForeignConstraint{
				{
					Columns:    []string{"customer_id"},
					ForeignKey: &mgmtv1alpha1.ForeignKey{Table: "public.customers", Columns: []string{"id"}},
				},
			}},
		},
	}

	require.Equal(t, int32(2), countReferencesTo(constraints, "public.customers", "id"))
	require.Equal(t, int32(0), countReferencesTo(constraints, "public.customers", "name"))
	require.Equal(t, int32(0), countReferencesTo(constraints, "public.orders", "customer_id"))
}
