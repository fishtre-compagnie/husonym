package schemamanager_postgres

import (
	"context"
	"testing"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	sqlmanager_shared "github.com/fishtre-compagnie/husonym/backend/pkg/sqlmanager/shared"
	shared "github.com/fishtre-compagnie/husonym/internal/schema-manager/shared"
	"github.com/fishtre-compagnie/husonym/internal/testutil"
	"github.com/stretchr/testify/require"
)

// The statements that reconcile the domains of a destination: a new domain is given its
// default once it is created, and a domain that differs has its constraints, its default and
// its nullability brought to those of the source.
func Test_BuildSchemaDiffStatements_Domains(t *testing.T) {
	manager := &PostgresSchemaManager{
		logger:   testutil.GetTestLogger(t),
		destOpts: &mgmtv1alpha1.PostgresDestinationConnectionOptions{InitTableSchema: true},
	}
	diff := &shared.SchemaDifferences{
		ExistsInSource: &shared.ExistsInSource{
			Domains: []*sqlmanager_shared.DomainDataType{
				{Schema: "app", Name: "created", Default: "nextval('app.created_seq'::regclass)"},
				{Schema: "app", Name: "plain"},
			},
		},
		ExistsInDestination: &shared.ExistsInDestination{
			Domains: []*sqlmanager_shared.DomainDataType{{Schema: "app", Name: "dropped"}},
		},
		ExistsInBoth: &shared.ExistsInBoth{
			Different: &shared.Different{
				Domains: []*shared.DomainDiff{
					{
						Domain:             &sqlmanager_shared.DomainDataType{Schema: "app", Name: "amount", IsNullable: true},
						IsNullDifferent:    true,
						IsDefaultDifferent: true,
						NewConstraints:     map[string]string{"changed": "CHECK ((VALUE < 1000))"},
						RemovedConstraints: []string{"removed", "changed"},
					},
					{Domain: &sqlmanager_shared.DomainDataType{Schema: "app", Name: "same"}},
				},
			},
		},
	}

	blocks, err := manager.BuildSchemaDiffStatements(context.Background(), diff)
	require.NoError(t, err)

	statements := map[string][]string{}
	for _, block := range blocks {
		statements[block.Label] = block.Statements
	}
	require.Equal(t, []string{`DROP DOMAIN IF EXISTS "app"."dropped";`}, statements[sqlmanager_shared.DropDatatypesLabel])
	require.Equal(t, []string{
		`ALTER DOMAIN "app"."created" SET DEFAULT nextval('app.created_seq'::regclass);`,
		`ALTER DOMAIN "app"."amount" DROP CONSTRAINT IF EXISTS "changed";`,
		`ALTER DOMAIN "app"."amount" DROP CONSTRAINT IF EXISTS "removed";`,
		`ALTER DOMAIN "app"."amount" ADD CONSTRAINT "changed" CHECK ((VALUE < 1000));`,
		`ALTER DOMAIN "app"."amount" DROP DEFAULT;`,
		`ALTER DOMAIN "app"."amount" DROP NOT NULL;`,
	}, statements[sqlmanager_shared.UpdateDatatypesLabel])
}
