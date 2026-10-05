package sqlmanager_postgres

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// The constraints removed are dropped before the new ones are added: a constraint whose
// definition changed is both, under one name. Each in the order of the names, whatever the
// order they are given in.
func Test_BuildDomainConstraintStatements(t *testing.T) {
	statements := BuildDomainConstraintStatements(
		"app", "amount",
		map[string]string{"changed": "CHECK ((VALUE < 1000))", "added": "CHECK ((VALUE <> 13))"},
		[]string{"removed", "changed"},
	)

	require.Equal(t, []string{
		`ALTER DOMAIN "app"."amount" DROP CONSTRAINT IF EXISTS "changed";`,
		`ALTER DOMAIN "app"."amount" DROP CONSTRAINT IF EXISTS "removed";`,
		`ALTER DOMAIN "app"."amount" ADD CONSTRAINT "added" CHECK ((VALUE <> 13));`,
		`ALTER DOMAIN "app"."amount" ADD CONSTRAINT "changed" CHECK ((VALUE < 1000));`,
	}, statements)

	require.Empty(t, BuildDomainConstraintStatements("app", "amount", nil, nil))
}

// The label of an enum is a value read from the catalog: it is written as one string literal.
func Test_BuildUpdateEnumStatements_WritesALabelAsOneLiteral(t *testing.T) {
	for label, literal := range map[string]string{
		"plain label_1": `'plain label_1'`,
		`o'clock`:       `'o''clock'`,
		`back\slash`:    `'back\slash'`,
		`quarter past'`: `'quarter past'''`,
	} {
		require.Equal(t,
			[]string{`ALTER TYPE "app"."mood" ADD VALUE IF NOT EXISTS ` + literal + `;`},
			BuildUpdateEnumStatements("app", "mood", []string{label}, nil),
		)
		require.Equal(t,
			[]string{`ALTER TYPE "app"."mood" RENAME VALUE ` + literal + ` TO 'calm';`},
			BuildUpdateEnumStatements("app", "mood", nil, map[string]string{label: "calm"}),
		)
		require.Equal(t,
			[]string{`ALTER TYPE "app"."mood" RENAME VALUE 'calm' TO ` + literal + `;`},
			BuildUpdateEnumStatements("app", "mood", nil, map[string]string{"calm": label}),
		)
	}
}

func Test_BuildUpdateDomainNotNullStatement(t *testing.T) {
	require.Equal(t, `ALTER DOMAIN "app"."amount" SET NOT NULL;`, BuildUpdateDomainNotNullStatement("app", "amount", false))
	require.Equal(t, `ALTER DOMAIN "app"."amount" DROP NOT NULL;`, BuildUpdateDomainNotNullStatement("app", "amount", true))
}
