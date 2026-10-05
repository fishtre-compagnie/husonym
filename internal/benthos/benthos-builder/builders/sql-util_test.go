package benthosbuilder_builders

import (
	"strings"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func Test_isSourceMissingColumns(t *testing.T) {
	t.Run("no missing columns", func(t *testing.T) {
		missing, ok := isSourceMissingColumnsFoundInMappings(
			map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow{
				"public.users": {
					"id":   {},
					"name": {},
				},
			},
			[]*mgmtv1alpha1.JobMapping{
				{
					Schema: "public",
					Table:  "users",
					Column: "id",
				},
				{
					Schema: "public",
					Table:  "users",
					Column: "name",
				},
			},
		)
		require.False(t, ok)
		require.Empty(t, missing)
	})

	t.Run("missing table", func(t *testing.T) {
		missing, ok := isSourceMissingColumnsFoundInMappings(
			map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow{
				"public.users": {
					"id": {},
				},
			},
			[]*mgmtv1alpha1.JobMapping{
				{
					Schema: "public",
					Table:  "accounts", // non-existent table
					Column: "id",
				},
				{
					Schema: "public",
					Table:  "accounts", // non-existent table
					Column: "name",
				},
			},
		)
		require.True(t, ok)
		require.ElementsMatch(t, []string{"public.accounts.id", "public.accounts.name"}, missing)
	})

	t.Run("missing column", func(t *testing.T) {
		missing, ok := isSourceMissingColumnsFoundInMappings(
			map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow{
				"public.users": {
					"id": {},
				},
			},
			[]*mgmtv1alpha1.JobMapping{
				{
					Schema: "public",
					Table:  "users",
					Column: "id",
				},
				{
					Schema: "public",
					Table:  "users",
					Column: "email", // non-existent column
				},
			},
		)
		require.True(t, ok)
		require.Equal(t, []string{"public.users.email"}, missing)
	})
}

func Test_removeMappingsNotFoundInSource(t *testing.T) {
	t.Run("removes mappings for non-existent tables", func(t *testing.T) {
		mappings := []*mgmtv1alpha1.JobMapping{
			{
				Schema: "public",
				Table:  "users",
				Column: "id",
			},
			{
				Schema: "public",
				Table:  "accounts", // non-existent table
				Column: "id",
			},
		}

		groupedSchemas := map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow{
			"public.users": {
				"id": {},
			},
		}

		result, removed := removeMappingsNotFoundInSource(mappings, groupedSchemas)

		require.Len(t, result, 1)
		require.Equal(t, "public", result[0].Schema)
		require.Equal(t, "users", result[0].Table)
		require.Equal(t, "id", result[0].Column)
		require.Equal(t, []*mgmtv1alpha1.JobMapping{mappings[1]}, removed)
	})

	t.Run("removes mappings for non-existent columns", func(t *testing.T) {
		mappings := []*mgmtv1alpha1.JobMapping{
			{
				Schema: "public",
				Table:  "users",
				Column: "id",
			},
			{
				Schema: "public",
				Table:  "users",
				Column: "email", // non-existent column
			},
		}

		groupedSchemas := map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow{
			"public.users": {
				"id": {},
			},
		}

		result, removed := removeMappingsNotFoundInSource(mappings, groupedSchemas)

		require.Len(t, result, 1)
		require.Equal(t, "public", result[0].Schema)
		require.Equal(t, "users", result[0].Table)
		require.Equal(t, "id", result[0].Column)
		require.Equal(t, []*mgmtv1alpha1.JobMapping{mappings[1]}, removed)
	})

	t.Run("keeps all mappings when everything exists", func(t *testing.T) {
		mappings := []*mgmtv1alpha1.JobMapping{
			{
				Schema: "public",
				Table:  "users",
				Column: "id",
			},
			{
				Schema: "public",
				Table:  "users",
				Column: "email",
			},
		}

		groupedSchemas := map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow{
			"public.users": {
				"id":    {},
				"email": {},
			},
		}

		result, removed := removeMappingsNotFoundInSource(mappings, groupedSchemas)

		require.Len(t, result, 2)
		require.Equal(t, mappings, result)
		require.Empty(t, removed)
	})

	t.Run("returns empty slice when no mappings exist in schema", func(t *testing.T) {
		mappings := []*mgmtv1alpha1.JobMapping{
			{
				Schema: "public",
				Table:  "users",
				Column: "id",
			},
		}

		groupedSchemas := map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow{
			"public.accounts": {
				"id": {},
			},
		}

		result, removed := removeMappingsNotFoundInSource(mappings, groupedSchemas)

		require.Empty(t, result)
		require.Equal(t, mappings, removed)
	})
}

func Test_checkSourceShowsTheJob(t *testing.T) {
	mapping := &mgmtv1alpha1.JobMapping{Schema: "public", Table: "users", Column: "id"}

	require.NoError(t, checkSourceShowsTheJob(nil, nil), "a job that maps nothing yet")
	require.NoError(t, checkSourceShowsTheJob(
		[]*mgmtv1alpha1.JobMapping{mapping, {Schema: "public", Table: "users", Column: "gone"}},
		[]*mgmtv1alpha1.JobMapping{mapping},
	), "a source that lost a column")
	require.ErrorIs(t, checkSourceShowsTheJob(
		[]*mgmtv1alpha1.JobMapping{mapping},
		nil,
	), errSourceShowsNoMappedColumn, "a source that shows none of the job")
}

func Test_withoutNullableColumns(t *testing.T) {
	columnInfo := map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow{
		"public.badge": {
			"code":   {IsNullable: true},
			"serial": {IsNullable: false},
			"site":   {IsNullable: false},
		},
	}
	uniqueKeys := map[string][][]string{
		"public.badge":   {{"code"}, {"site", "code"}, {"site", "serial"}, {"unknown"}},
		"public.missing": {{"id"}},
	}

	require.Equal(t, map[string][][]string{
		"public.badge": {{"site", "serial"}},
	}, withoutNullableColumns(uniqueKeys, columnInfo))
}

func Test_formatMappingColumns(t *testing.T) {
	t.Run("names a column the way the halt message does", func(t *testing.T) {
		require.Equal(t, []string{"public.users.email"}, formatMappingColumns(
			[]*mgmtv1alpha1.JobMapping{
				{Schema: "public", Table: "users", Column: "email"},
			},
		))
	})

	t.Run("sorts, because the mappings come from walking a map", func(t *testing.T) {
		require.Equal(t, []string{
			"public.users.email",
			"public.users.phone",
			"sales.orders.total",
		}, formatMappingColumns([]*mgmtv1alpha1.JobMapping{
			{Schema: "sales", Table: "orders", Column: "total"},
			{Schema: "public", Table: "users", Column: "phone"},
			{Schema: "public", Table: "users", Column: "email"},
		}))
	})

	t.Run("no mappings, no columns", func(t *testing.T) {
		require.Empty(t, formatMappingColumns(nil))
	})
}

func Test_autoMapNewColumns(t *testing.T) {
	passthrough := func(schema, table, column string) *mgmtv1alpha1.JobMapping {
		return &mgmtv1alpha1.JobMapping{
			Schema: schema, Table: table, Column: column,
			Transformer: &mgmtv1alpha1.JobMappingTransformer{
				Config: &mgmtv1alpha1.TransformerConfig{
					Config: &mgmtv1alpha1.TransformerConfig_PassthroughConfig{
						PassthroughConfig: &mgmtv1alpha1.Passthrough{},
					},
				},
			},
		}
	}
	generateDefault := func(schema, table, column string) *mgmtv1alpha1.JobMapping {
		return &mgmtv1alpha1.JobMapping{
			Schema: schema, Table: table, Column: column,
			Transformer: &mgmtv1alpha1.JobMappingTransformer{
				Config: &mgmtv1alpha1.TransformerConfig{
					Config: &mgmtv1alpha1.TransformerConfig_GenerateDefaultConfig{
						GenerateDefaultConfig: &mgmtv1alpha1.GenerateDefault{},
					},
				},
			},
		}
	}

	columnInfo := map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow{
		"public.users": {
			"email":           {DataType: "character varying(255)"},
			"telephone":       {DataType: "character varying(20)"},
			"login":           {DataType: "character varying(50)"},
			"champ_libre":     {DataType: "text"},
			"email_normalise": {DataType: "character varying(255)"},
		},
	}
	constraints := &sqlmanager_shared.TableConstraints{
		UniqueConstraints: map[string][][]string{"public.users": {{"login"}}},
	}

	configOf := func(mappings []*mgmtv1alpha1.JobMapping, column string) *mgmtv1alpha1.TransformerConfig {
		for _, m := range mappings {
			if m.GetColumn() == column {
				return m.GetTransformer().GetConfig()
			}
		}
		t.Fatalf("no mapping for %s", column)
		return nil
	}

	t.Run("maps a recognised column as the catalogue does, and passes the rest through", func(t *testing.T) {
		out, anonymized, passedThrough := autoMapNewColumns([]*mgmtv1alpha1.JobMapping{
			passthrough("public", "users", "email"),
			passthrough("public", "users", "telephone"),
			passthrough("public", "users", "champ_libre"),
		}, columnInfo, constraints, nil, true)

		require.NotNil(t, configOf(out, "email").GetGenerateEmailConfig())
		// The catalogue's config, not an empty one: the phone keeps its format.
		require.True(t, configOf(out, "telephone").GetTransformPhoneNumberConfig().GetPreserveFormat())
		require.NotNil(t, configOf(out, "champ_libre").GetPassthroughConfig())
		require.Equal(t, []string{"public.users.email (email)", "public.users.telephone (phone_number)"}, anonymized)
		require.Equal(t, []string{"public.users.champ_libre"}, passedThrough.columns)
		require.Zero(t, passedThrough.sensitive)
	})

	t.Run("a unique column stays in passthrough, even when recognised", func(t *testing.T) {
		out, anonymized, passedThrough := autoMapNewColumns([]*mgmtv1alpha1.JobMapping{
			passthrough("public", "users", "login"),
			passthrough("public", "users", "champ_libre"),
		}, columnInfo, constraints, nil, true)

		require.NotNil(t, configOf(out, "login").GetPassthroughConfig())
		require.Empty(t, anonymized)
		// A sensitive column is named with its category and with why it stays as it is.
		require.Equal(t, []string{"public.users.champ_libre", "public.users.login (username, covered by a key)"}, passedThrough.columns)
		require.Equal(t, 1, passedThrough.sensitive)
	})

	t.Run("the warning counts the personal data that passed through", func(t *testing.T) {
		_, _, passedThrough := autoMapNewColumns([]*mgmtv1alpha1.JobMapping{
			passthrough("public", "users", "login"),
			passthrough("public", "users", "champ_libre"),
		}, columnInfo, constraints, nil, true)
		require.Equal(t,
			"2 unmapped columns passed through as is, awaiting review, 1 of them personal data "+
				"(named with the category and the reason): "+
				"[public.users.champ_libre, public.users.login (username, covered by a key)]",
			passedThroughWarning(passedThrough),
		)
	})

	t.Run("a column under a CHECK constraint stays in passthrough", func(t *testing.T) {
		people := map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow{
			"public.people": {
				"dob":        {DataType: "character varying(10)"},
				"dob_text":   {DataType: "text"},
				"Phone":      {DataType: "text"},
				"email":      {DataType: "text"},
				"channel":    {DataType: "text"},
				"salary":     {DataType: "integer"},
				"first_name": {DataType: "text"},
			},
			"shop.people": {
				"dob":   {DataType: "varchar(10)"},
				"email": {DataType: "varchar(255)"},
			},
		}
		checks := &sqlmanager_shared.TableConstraints{CheckConstraints: map[string][]string{
			"public.people": {
				`CHECK (((dob)::text ~ '^\d{4}-\d{2}-\d{2}$'::text))`,
				`CHECK (("Phone" ~ '^\+'::text))`,
				// The name of a column inside a text is not the column.
				`CHECK ((channel = ANY (ARRAY['email'::text, 'first_name''s'::text])))`,
				`CHECK (((salary >= 0) AND (salary < 100000)))`,
			},
			"shop.people": {"regexp_like(`dob`,_utf8mb4'^[0-9]{4}-[0-9]{2}-[0-9]{2}$')"},
		}}
		var mappings []*mgmtv1alpha1.JobMapping
		for table, columns := range people {
			schema, name, _ := strings.Cut(table, ".")
			for column := range columns {
				mappings = append(mappings, passthrough(schema, name, column))
			}
		}
		_, anonymized, passedThrough := autoMapNewColumns(mappings, people, checks, nil, true)

		require.Equal(t, []string{
			"public.people.Phone (phone_number, under a CHECK constraint)",
			"public.people.channel",
			"public.people.dob (birth_date, under a CHECK constraint)",
			"public.people.salary (salary, under a CHECK constraint)",
			"shop.people.dob (birth_date, under a CHECK constraint)",
		}, passedThrough.columns)
		require.Equal(t, 4, passedThrough.sensitive)
		require.Equal(t, []string{
			"public.people.dob_text (birth_date)", "public.people.email (email)",
			"public.people.first_name (person_first_name)", "shop.people.email (email)",
		}, anonymized)
	})

	t.Run("without a derivation key, the phone is anonymized without keeping its format", func(t *testing.T) {
		// The option needs a key the run would not have: writing it to the job would fail
		// that run and every one after it on the column AutoMap had just mapped.
		out, anonymized, _ := autoMapNewColumns([]*mgmtv1alpha1.JobMapping{
			passthrough("public", "users", "telephone"),
		}, columnInfo, constraints, nil, false)

		phone := configOf(out, "telephone").GetTransformPhoneNumberConfig()
		require.NotNil(t, phone, "the column is still anonymized")
		require.False(t, phone.GetPreserveFormat())
		require.Equal(t, []string{"public.users.telephone (phone_number)"}, anonymized)
	})

	t.Run("a secret, an identifier, a salary and an age are rewritten, each by its type", func(t *testing.T) {
		accounts := map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow{
			"public.accounts": {
				"user_pass":               {DataType: "character varying(72)"},
				"email_verification_code": {DataType: "character(6)"},
				"api_key":                 {DataType: "text"},
				"tax_id":                  {DataType: "character varying(20)"},
				"passnummer":              {DataType: "bigint"},
				"iban":                    {DataType: "character varying(34)"},
				"ethnicity":               {DataType: "text"},
				"salary":                  {DataType: "numeric(10,2)"},
				"age":                     {DataType: "smallint"},
				"national_id":             {DataType: "character varying(20)"},
				"untyped_secret":          nil,
				"refresh_token":           {DataType: "jsonb"},
				"session_token":           {DataType: "uuid"},
				"pay_grade_salary":        {DataType: "smallint"},
				"age_ratio":               {DataType: "numeric(3,2)"},
			},
		}
		// A national identifier under a unique constraint is a key: it stays as it is.
		unique := &sqlmanager_shared.TableConstraints{
			UniqueConstraints: map[string][][]string{"public.accounts": {{"national_id"}}},
		}
		var mappings []*mgmtv1alpha1.JobMapping
		for column := range accounts["public.accounts"] {
			mappings = append(mappings, passthrough("public", "accounts", column))
		}
		out, anonymized, passedThrough := autoMapNewColumns(mappings, accounts, unique, nil, true)

		for _, column := range []string{"user_pass", "email_verification_code", "api_key", "tax_id", "iban", "ethnicity"} {
			require.NotNil(t, configOf(out, column).GetTransformCharacterScrambleConfig(), column)
		}
		require.NotNil(t, configOf(out, "passnummer").GetGenerateInt64Config())
		require.InDelta(t, 20000, configOf(out, "salary").GetGenerateFloat64Config().GetMin(), 0)
		require.InDelta(t, 90000, configOf(out, "salary").GetGenerateFloat64Config().GetMax(), 0)
		require.EqualValues(t, 18, configOf(out, "age").GetGenerateInt64Config().GetMin())
		require.EqualValues(t, 90, configOf(out, "age").GetGenerateInt64Config().GetMax())
		// A token in a uuid column gets another uuid.
		require.NotNil(t, configOf(out, "session_token").GetGenerateUuidConfig())
		// The range is cut at what the column holds.
		require.EqualValues(t, 20000, configOf(out, "pay_grade_salary").GetGenerateInt64Config().GetMin())
		require.EqualValues(t, 32767, configOf(out, "pay_grade_salary").GetGenerateInt64Config().GetMax())
		require.Len(t, anonymized, 11)
		// Left as they are: the key, and the columns whose type no transformer writes or the
		// run does not know.
		require.Equal(t, []string{
			"public.accounts.age_ratio",
			"public.accounts.national_id (national_id, covered by a key)",
			"public.accounts.refresh_token (secret, no transformer for its type)",
			"public.accounts.untyped_secret (secret, no transformer for its type)",
		}, passedThrough.columns)
		require.Equal(t, 3, passedThrough.sensitive)
	})

	t.Run("the character scramble has no option to turn off without a derivation key", func(t *testing.T) {
		accounts := map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow{
			"public.accounts": {"tax_id": {DataType: "character varying(20)"}},
		}
		with, _, _ := autoMapNewColumns([]*mgmtv1alpha1.JobMapping{passthrough("public", "accounts", "tax_id")}, accounts, nil, nil, true)
		without, _, _ := autoMapNewColumns([]*mgmtv1alpha1.JobMapping{passthrough("public", "accounts", "tax_id")}, accounts, nil, nil, false)
		require.NotNil(t, configOf(without, "tax_id").GetTransformCharacterScrambleConfig())
		require.True(t, proto.Equal(configOf(with, "tax_id"), configOf(without, "tax_id")))
	})

	t.Run("a column the destination recomputes keeps its GenerateDefault", func(t *testing.T) {
		out, anonymized, passedThrough := autoMapNewColumns([]*mgmtv1alpha1.JobMapping{
			generateDefault("public", "users", "email_normalise"),
		}, columnInfo, constraints, nil, true)

		require.NotNil(t, configOf(out, "email_normalise").GetGenerateDefaultConfig())
		require.Empty(t, anonymized)
		require.Empty(t, passedThrough.columns)
	})
}

func Test_constrainedColumns(t *testing.T) {
	constrained := constrainedColumns(&sqlmanager_shared.TableConstraints{
		PrimaryKeyConstraints: map[string][]string{"public.users": {"id"}},
		ForeignKeyConstraints: map[string][]*sqlmanager_shared.ForeignConstraint{
			"public.orders": {{
				Columns:    []string{"user_id"},
				ForeignKey: &sqlmanager_shared.ForeignKey{Table: "public.users", Columns: []string{"code"}},
			}},
		},
		UniqueIndexes: map[string][][]string{"public.users": {{"email", "tenant"}}},
	}, nil)
	for _, c := range []struct{ table, column string }{
		{"public.users", "id"},
		{"public.orders", "user_id"},
		{"public.users", "code"},
		{"public.users", "email"},
		{"public.users", "tenant"},
	} {
		_, ok := constrained[c.table][c.column]
		require.Truef(t, ok, "%s.%s", c.table, c.column)
	}
	_, ok := constrained["public.users"]["telephone"]
	require.False(t, ok)
	require.Empty(t, constrainedColumns(nil, nil))

	// A virtual foreign key constrains both of its sides, as a real one does.
	virtual := constrainedColumns(nil, []*mgmtv1alpha1.VirtualForeignConstraint{{
		Schema: "public", Table: "orders", Columns: []string{"buyer_email"},
		ForeignKey: &mgmtv1alpha1.VirtualForeignKey{Schema: "public", Table: "users", Columns: []string{"email"}},
	}})
	_, ok = virtual["public.orders"]["buyer_email"]
	require.True(t, ok)
	_, ok = virtual["public.users"]["email"]
	require.True(t, ok)
}

func virtualForeignKeyFixture() (
	map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow,
	*mgmtv1alpha1.VirtualForeignConstraint,
) {
	source := map[string]map[string]*sqlmanager_shared.DatabaseSchemaRow{
		"public.orders": {"user_id": {IsNullable: false}, "note": {IsNullable: true}},
		"public.users":  {"id": {}, "email": {}},
	}
	fk := &mgmtv1alpha1.VirtualForeignConstraint{
		Schema: "public", Table: "orders", Columns: []string{"user_id"},
		ForeignKey: &mgmtv1alpha1.VirtualForeignKey{Schema: "public", Table: "users", Columns: []string{"id"}},
	}
	return source, fk
}

func Test_mergeVirtualForeignKeys_AddsAKeyWhoseColumnsTheSourceHolds(t *testing.T) {
	source, fk := virtualForeignKeyFixture()
	existing := &sqlmanager_shared.ForeignConstraint{
		Columns:    []string{"note"},
		ForeignKey: &sqlmanager_shared.ForeignKey{Table: "public.users", Columns: []string{"email"}},
	}

	merged, err := mergeVirtualForeignKeys(
		map[string][]*sqlmanager_shared.ForeignConstraint{"public.orders": {existing}},
		[]*mgmtv1alpha1.VirtualForeignConstraint{fk},
		source,
	)

	require.NoError(t, err)
	require.Len(t, merged["public.orders"], 2)
	require.Same(t, existing, merged["public.orders"][0])
	added := merged["public.orders"][1]
	require.Equal(t, []string{"user_id"}, added.Columns)
	require.Equal(t, []bool{true}, added.NotNullable)
	require.Equal(t, "public.users", added.ForeignKey.Table)
	require.Equal(t, []string{"id"}, added.ForeignKey.Columns)
}

func Test_mergeVirtualForeignKeys_ChildColumnAbsentFromTheSource(t *testing.T) {
	source, fk := virtualForeignKeyFixture()
	fk.Columns = []string{"buyer"}

	_, err := mergeVirtualForeignKeys(nil, []*mgmtv1alpha1.VirtualForeignConstraint{fk}, source)

	require.EqualError(t, err, "virtual foreign key source column not found: public.orders.buyer")
}

func Test_mergeVirtualForeignKeys_ReferencedColumnAbsentFromTheSource(t *testing.T) {
	source, fk := virtualForeignKeyFixture()
	fk.ForeignKey.Columns = []string{"id", "tenant"}

	_, err := mergeVirtualForeignKeys(nil, []*mgmtv1alpha1.VirtualForeignConstraint{fk}, source)

	require.EqualError(t, err,
		"virtual foreign key of public.orders references column tenant of public.users, which the source does not hold")
}

func Test_mergeVirtualForeignKeys_ReferencedTableAbsentFromTheSource(t *testing.T) {
	source, fk := virtualForeignKeyFixture()
	fk.ForeignKey.Table = "accounts"

	_, err := mergeVirtualForeignKeys(nil, []*mgmtv1alpha1.VirtualForeignConstraint{fk}, source)

	require.EqualError(t, err,
		"virtual foreign key of public.orders references table public.accounts, which the source does not hold")
}
