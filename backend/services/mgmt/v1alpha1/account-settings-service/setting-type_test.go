package v1alpha1_accountsettingservice

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/stretchr/testify/require"
)

// The kind of a setting is written in three places that must say the same thing: the
// variant of the oneof, the key protojson writes into the column, and the CASE of the
// generated column. Nothing in the compiler ties them together, so these tests do — a
// variant added without its line in the migration would land a row the constraint refuses,
// at the first write, in production.

func TestTheStoredConfigUsesTheKeyTheMigrationReads(t *testing.T) {
	stored, err := json.Marshal(&mgmtv1alpha1.AccountSettingConfig{
		Config: &mgmtv1alpha1.AccountSettingConfig_AnonymizationConsistency{
			AnonymizationConsistency: &mgmtv1alpha1.AnonymizationConsistency{
				DerivationKey: "the-key",
			},
		},
	})
	require.NoError(t, err)
	require.Contains(
		t,
		string(stored),
		`"anonymizationConsistency"`,
		"protojson writes the variant in lowerCamelCase, which is what the generated column looks for",
	)
}

func TestEverySettingTypeHasItsLineInTheMigration(t *testing.T) {
	schema := readSchema(t)

	oneof := (&mgmtv1alpha1.AccountSettingConfig{}).
		ProtoReflect().Descriptor().Oneofs().ByName("config")
	require.NotNil(t, oneof)

	for i := range oneof.Fields().Len() {
		field := oneof.Fields().Get(i)
		require.Containsf(
			t,
			schema,
			fmt.Sprintf("config->'%s'", field.JSONName()),
			"the generated column of account_settings does not recognize the variant %s",
			field.Name(),
		)
		require.Containsf(
			t,
			schema,
			fmt.Sprintf("'%s'", field.Name()),
			"the generated column of account_settings does not name the type %s",
			field.Name(),
		)
	}
}

func TestTheNameThisServiceQueriesIsTheVariantsOwn(t *testing.T) {
	field := (&mgmtv1alpha1.AccountSettingConfig{}).
		ProtoReflect().Descriptor().Oneofs().ByName("config").
		Fields().ByName("anonymization_consistency")
	require.NotNil(t, field)
	require.Equal(t, settingTypeAnonymizationConsistency, string(field.Name()))
}

// readSchema returns every migration that goes up, so that a later one changing the
// generated column counts as much as the one that created it.
func readSchema(t *testing.T) string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("..", "..", "..", "..", "sql", "postgresql", "schema", "*.up.sql"))
	require.NoError(t, err)
	require.NotEmpty(t, paths)

	var schema strings.Builder
	for _, path := range paths {
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		schema.Write(content)
	}
	return schema.String()
}
