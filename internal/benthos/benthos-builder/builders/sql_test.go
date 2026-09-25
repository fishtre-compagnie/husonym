package benthosbuilder_builders

import (
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	rc "github.com/fishtre-compagnie/husonym/internal/runconfigs"
	"github.com/stretchr/testify/require"
)

func TestFilterForeignKeysMap(t *testing.T) {
	tests := []struct {
		name              string
		colTransformerMap map[string]map[string]*mgmtv1alpha1.JobMappingTransformer
		foreignKeysMap    map[string][]*sqlmanager_shared.ForeignConstraint
		expected          map[string][]*sqlmanager_shared.ForeignConstraint
	}{
		{
			name:              "Empty input maps",
			colTransformerMap: map[string]map[string]*mgmtv1alpha1.JobMappingTransformer{},
			foreignKeysMap:    map[string][]*sqlmanager_shared.ForeignConstraint{},
			expected:          map[string][]*sqlmanager_shared.ForeignConstraint{},
		},
		{
			name: "No matching tables",
			colTransformerMap: map[string]map[string]*mgmtv1alpha1.JobMappingTransformer{
				"table1": {"col1": &mgmtv1alpha1.JobMappingTransformer{}},
			},
			foreignKeysMap: map[string][]*sqlmanager_shared.ForeignConstraint{
				"table2": {
					{
						Columns:     []string{"col1"},
						NotNullable: []bool{true},
						ForeignKey: &sqlmanager_shared.ForeignKey{
							Table:   "ref_table",
							Columns: []string{"ref_col"},
						},
					},
				},
			},
			expected: map[string][]*sqlmanager_shared.ForeignConstraint{},
		},
		{
			name: "Filtered composite foreign keys",
			colTransformerMap: map[string]map[string]*mgmtv1alpha1.JobMappingTransformer{
				"table1": {
					"col1": &mgmtv1alpha1.JobMappingTransformer{
						Config: &mgmtv1alpha1.TransformerConfig{
							Config: &mgmtv1alpha1.TransformerConfig_Nullconfig{},
						},
					},
					"col2": &mgmtv1alpha1.JobMappingTransformer{
						Config: &mgmtv1alpha1.TransformerConfig{
							Config: &mgmtv1alpha1.TransformerConfig_Nullconfig{},
						},
					},
					"col3": &mgmtv1alpha1.JobMappingTransformer{
						Config: &mgmtv1alpha1.TransformerConfig{
							Config: &mgmtv1alpha1.TransformerConfig_Nullconfig{},
						},
					},
				},
			},
			foreignKeysMap: map[string][]*sqlmanager_shared.ForeignConstraint{
				"table1": {
					{
						Columns:     []string{"col1", "col2", "col3"},
						NotNullable: []bool{false, true, true},
						ForeignKey: &sqlmanager_shared.ForeignKey{
							Table:   "ref_table",
							Columns: []string{"ref_col1", "ref_col2", "ref_col3"},
						},
					},
				},
			},
			expected: map[string][]*sqlmanager_shared.ForeignConstraint{
				"table1": {
					{
						Columns:     []string{"col2", "col3"},
						NotNullable: []bool{true, true},
						ForeignKey: &sqlmanager_shared.ForeignKey{
							Table:   "ref_table",
							Columns: []string{"ref_col2", "ref_col3"},
						},
					},
				},
			},
		},
		{
			name: "Filtered foreign keys",
			colTransformerMap: map[string]map[string]*mgmtv1alpha1.JobMappingTransformer{
				"table1": {
					"col1": &mgmtv1alpha1.JobMappingTransformer{
						Config: &mgmtv1alpha1.TransformerConfig{
							Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{},
						},
					},
				},
				"table2": {
					"col2": &mgmtv1alpha1.JobMappingTransformer{
						Config: &mgmtv1alpha1.TransformerConfig{
							Config: &mgmtv1alpha1.TransformerConfig_Nullconfig{},
						},
					},
				},
			},
			foreignKeysMap: map[string][]*sqlmanager_shared.ForeignConstraint{
				"table1": {
					{
						Columns:     []string{"col1"},
						NotNullable: []bool{false},
						ForeignKey: &sqlmanager_shared.ForeignKey{
							Table:   "ref_table",
							Columns: []string{"ref_col1"},
						},
					},
				},
				"table2": {
					{
						Columns:     []string{"col2"},
						NotNullable: []bool{false},
						ForeignKey: &sqlmanager_shared.ForeignKey{
							Table:   "ref_table",
							Columns: []string{"ref_col2"},
						},
					},
				},
			},
			expected: map[string][]*sqlmanager_shared.ForeignConstraint{
				"table1": {
					{
						Columns:     []string{"col1"},
						NotNullable: []bool{false},
						ForeignKey: &sqlmanager_shared.ForeignKey{
							Table:   "ref_table",
							Columns: []string{"ref_col1"},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterForeignKeysMap(tt.colTransformerMap, tt.foreignKeysMap)
			require.Equal(t, tt.expected, result)
		})
	}
}

func Test_isNullJobMappingTransformer(t *testing.T) {
	t.Run("yes", func(t *testing.T) {
		actual := isNullJobMappingTransformer(&mgmtv1alpha1.JobMappingTransformer{
			Config: &mgmtv1alpha1.TransformerConfig{
				Config: &mgmtv1alpha1.TransformerConfig_Nullconfig{},
			},
		})
		require.True(t, actual)
	})
	t.Run("no", func(t *testing.T) {
		actual := isNullJobMappingTransformer(&mgmtv1alpha1.JobMappingTransformer{
			Config: &mgmtv1alpha1.TransformerConfig{
				Config: &mgmtv1alpha1.TransformerConfig_GenerateStringConfig{},
			},
		})
		require.False(t, actual)
	})
	t.Run("nil", func(t *testing.T) {
		actual := isNullJobMappingTransformer(nil)
		require.False(t, actual)
	})
}

func Test_isDefaultJobMappingTransformer(t *testing.T) {
	t.Run("yes", func(t *testing.T) {
		actual := isDefaultJobMappingTransformer(&mgmtv1alpha1.JobMappingTransformer{
			Config: &mgmtv1alpha1.TransformerConfig{
				Config: &mgmtv1alpha1.TransformerConfig_GenerateDefaultConfig{},
			},
		})
		require.True(t, actual)
	})
	t.Run("no", func(t *testing.T) {
		actual := isDefaultJobMappingTransformer(&mgmtv1alpha1.JobMappingTransformer{
			Config: &mgmtv1alpha1.TransformerConfig{
				Config: &mgmtv1alpha1.TransformerConfig_GenerateStringConfig{},
			},
		})
		require.False(t, actual)
	})
	t.Run("nil", func(t *testing.T) {
		actual := isDefaultJobMappingTransformer(nil)
		require.False(t, actual)
	})
}

// The default of a mandatory key says "no parent" only when it is a value. Trimming the
// quotes alone left the type PostgreSQL reports a default with ('XX'::text -> XX'::text),
// a sentinel no row ever matches — and the rows meant to be spared were deleted instead.
func Test_noParentValue(t *testing.T) {
	cases := []struct {
		driver, columnDefault, want string
		ok                          bool
	}{
		{sqlmanager_shared.PostgresDriver, `'XX'::text`, "XX", true},
		{sqlmanager_shared.PostgresDriver, `'XX'::character varying`, "XX", true},
		{sqlmanager_shared.PostgresDriver, `'it''s'::text`, "it's", true},
		{sqlmanager_shared.PostgresDriver, `0`, "0", true},
		{sqlmanager_shared.PostgresDriver, `-1`, "-1", true},
		{sqlmanager_shared.PostgresDriver, `nextval('t_id_seq'::regclass)`, "", false},
		{sqlmanager_shared.PostgresDriver, `gen_random_uuid()`, "", false},
		{sqlmanager_shared.PostgresDriver, `CURRENT_TIMESTAMP`, "", false},
		{sqlmanager_shared.PostgresDriver, ``, "", false},
		{sqlmanager_shared.MysqlDriver, `XX`, "XX", true},
		{sqlmanager_shared.MysqlDriver, `0`, "0", true},
		{sqlmanager_shared.MysqlDriver, `(uuid())`, "", false},
		{sqlmanager_shared.MssqlDriver, `(('XX'))`, "XX", true},
		{sqlmanager_shared.MssqlDriver, `((0))`, "0", true},
		{sqlmanager_shared.MssqlDriver, `(getdate())`, "", false},
	}
	for _, c := range cases {
		got, ok := noParentValue(c.driver, c.columnDefault)
		require.Equal(t, c.ok, ok, "%s %q", c.driver, c.columnDefault)
		require.Equal(t, c.want, got, "%s %q", c.driver, c.columnDefault)
	}
}

// filterForeignKeysMap takes out of a key the columns a null transformer writes NULL. What
// is left of a composite key reads as mandatory — every column it kept refuses NULL —
// while the key itself, holding a NULL, references nothing (MATCH SIMPLE) and the database
// never enforces it. The plan must leave such a key out rather than delete rows over it.
func Test_reducedKey(t *testing.T) {
	declared := []*sqlmanager_shared.ForeignConstraint{
		{
			Columns:     []string{"tenant_id", "owner_id"},
			NotNullable: []bool{true, false},
			ForeignKey:  &sqlmanager_shared.ForeignKey{Table: "public.owners", Columns: []string{"tenant_id", "id"}},
		},
		{
			Columns:     []string{"account_id"},
			NotNullable: []bool{true},
			ForeignKey:  &sqlmanager_shared.ForeignKey{Table: "public.accounts", Columns: []string{"id"}},
		},
	}

	reduced := &rc.ForeignKey{
		Columns: []string{"tenant_id"}, NotNullable: []bool{true},
		ReferenceSchema: "public", ReferenceTable: "owners", ReferenceColumns: []string{"tenant_id"},
	}
	require.True(t, reducedKey(declared, reduced), "owner_id was written NULL: the key is not enforced")

	whole := &rc.ForeignKey{
		Columns: []string{"tenant_id", "owner_id"}, NotNullable: []bool{true, false},
		ReferenceSchema: "public", ReferenceTable: "owners", ReferenceColumns: []string{"tenant_id", "id"},
	}
	require.False(t, reducedKey(declared, whole), "the key as it was declared")

	single := &rc.ForeignKey{
		Columns: []string{"account_id"}, NotNullable: []bool{true},
		ReferenceSchema: "public", ReferenceTable: "accounts", ReferenceColumns: []string{"id"},
	}
	require.False(t, reducedKey(declared, single))

	unknownParent := &rc.ForeignKey{
		Columns: []string{"tenant_id"}, NotNullable: []bool{true},
		ReferenceSchema: "public", ReferenceTable: "ailleurs", ReferenceColumns: []string{"id"},
	}
	require.False(t, reducedKey(declared, unknownParent), "a virtual key the database does not declare is kept")
}

// The destination wins in the merged columns, and leaves those of the source as they are:
// the builder compares the two afterwards.
func Test_mergeSourceDestinationColumnInfo_KeepsTheSource(t *testing.T) {
	source := map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow{
		"public.article": {"libelle": {CharacterMaximumLength: 40}},
	}
	merged := mergeSourceDestinationColumnInfo(source, map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow{
		"public.article": {"libelle": {CharacterMaximumLength: 5}},
	})
	require.Equal(t, 5, merged["public.article"]["libelle"].CharacterMaximumLength)
	require.Equal(t, 40, source["public.article"]["libelle"].CharacterMaximumLength)
}
