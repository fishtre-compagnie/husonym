package cases

import (
	"strings"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"
	"github.com/fishtre-compagnie/husonym/bench/schema"
)

// rightsCases run with accounts that lack what their role needs. The decision is that a
// run stops at its start on a blocking finding of its pre-flight check, with a message
// saying which privilege is missing; failing later on the first refused statement, or retrying it for
// minutes, is a gap. (The read-only source is not a case: every run of the bench reads a
// source set to refuse writes.)
func rightsCases() []*Case {
	return []*Case{
		rightsDestinationReadOnlyAccount(),
		rightsDestinationCannotSuspendForeignKeys(),
		rightsDestinationSufficientByRole(),
		rightsDestinationReadOnlyByRole(),
		rightsDestinationCannotTruncate(),
		rightsDestinationCannotSuspendTriggers(),
		rightsDestinationTriggerForeignDefiner(),
	}
}

// writeGrants are the privileges a destination account needs to write a job's tables, on
// each database, short of what a case takes away: emptying them, and on PostgreSQL
// suspending foreign keys, which Athanor needs. MySQL's TRIGGER is among them: without it
// an account does not even see the triggers the run must take out of its way.
var writeGrants = map[schema.Dialect][]string{
	schema.MySQL: {"GRANT SELECT, INSERT, UPDATE, DELETE, TRIGGER ON {db}.* TO {user}"},
	schema.Postgres: {
		"GRANT USAGE ON SCHEMA {db} TO {user}",
		"GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA {db} TO {user}",
		"GRANT SET ON PARAMETER session_replication_role TO {user}",
	},
}

// throughRole grants what grants give to {user} to a role instead, and the role to {user}:
// the account holds nothing in its own name. On MySQL the role is made a default one, the
// way an administrator hands out roles, so that a new session has it active.
func throughRole(role string, grants map[schema.Dialect][]string) map[schema.Dialect][]string {
	mysqlRole := "'" + role + "'"
	byRole := map[schema.Dialect][]string{
		schema.MySQL: {"DROP ROLE IF EXISTS " + mysqlRole, "CREATE ROLE " + mysqlRole},
		schema.Postgres: {
			"DO $husonym$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = '" + role + "') THEN " +
				"CREATE ROLE " + role + " NOLOGIN; END IF; END $husonym$",
		},
	}
	names := map[schema.Dialect]string{schema.MySQL: mysqlRole, schema.Postgres: role}
	for dialect, statements := range grants {
		for _, grant := range statements {
			byRole[dialect] = append(byRole[dialect], strings.ReplaceAll(grant, "{user}", names[dialect]))
		}
	}
	byRole[schema.MySQL] = append(byRole[schema.MySQL], "GRANT "+mysqlRole+" TO {user}", "SET DEFAULT ROLE ALL TO {user}")
	byRole[schema.Postgres] = append(byRole[schema.Postgres], "GRANT "+role+" TO {user}")
	return byRole
}

// rightsDestinationSufficientByRole: the account holds, through a role, exactly what a run
// needs. The check must ask the account, not read what was granted to it by name: it
// must let the run through.
func rightsDestinationSufficientByRole() *Case {
	tables, seed := articlesToWrite()
	return &Case{
		ID:                "rights-destination-sufficient-by-role",
		Priority:          P2,
		Title:             "Destination dont le compte tient ses droits d'un rôle, suffisants : le run passe",
		Tables:            tables,
		DestinationGrants: throughRole("bench_role_sufficient", writeGrants),
		Seed:              seed,
	}
}

// rightsDestinationReadOnlyByRole: the same, with a role that may only read. MySQL showed
// nothing of a role in the privilege tables the check used to read, and let the run go on
// to its first refused INSERT.
func rightsDestinationReadOnlyByRole() *Case {
	tables, seed := articlesToWrite()
	return &Case{
		ID:       "rights-destination-read-only-by-role",
		Priority: P2,
		//nolint:misspell // titre du rapport, rédigé en français
		Title:  "Destination dont le compte ne tient d'un rôle que la lecture : arrêt au démarrage du run",
		Tables: tables,
		DestinationGrants: throughRole("bench_role_read_only", map[schema.Dialect][]string{
			schema.MySQL:    {"GRANT SELECT ON {db}.* TO {user}"},
			schema.Postgres: {"GRANT USAGE ON SCHEMA {db} TO {user}", "GRANT SELECT ON ALL TABLES IN SCHEMA {db} TO {user}"},
		}),
		ExpectRunError: map[schema.Dialect]string{schema.MySQL: "cannot write", schema.Postgres: "cannot write"},
		Seed:           seed,
	}
}

// rightsDestinationCannotTruncate: the job empties the destination first, which the
// account may not do (TRUNCATE on PostgreSQL, DROP on MySQL). It used to fail on the
// statement, past the check, with the database's words only.
func rightsDestinationCannotTruncate() *Case {
	tables, seed := articlesToWrite()
	return &Case{
		ID:       "rights-destination-cannot-truncate",
		Priority: P2,
		//nolint:misspell // titre du rapport, rédigé en français
		Title:             "Destination à vider que le compte ne peut pas vider : arrêt au démarrage du run",
		Tables:            tables,
		Job:               Job{TruncateBeforeInsert: true},
		DestinationGrants: writeGrants,
		ExpectRunError:    map[schema.Dialect]string{schema.MySQL: "cannot empty", schema.Postgres: "cannot empty"},
		Seed:              seed,
	}
}

// rightsDestinationCannotSuspendTriggers: the destination holds the history trigger, and
// the account cannot take it out of the way: on PostgreSQL it does not own the table; on
// MySQL it lacks TRIGGER, which also hides the trigger from it — the run saw no trigger,
// suspended nothing, and the trigger wrote rows of its own into the copy.
func rightsDestinationCannotSuspendTriggers() *Case {
	c := triggerWritesSyncedTable("rights-destination-cannot-suspend-triggers",
		//nolint:misspell // titre du rapport, rédigé en français
		"Destination à trigger que le compte ne peut pas suspendre : arrêt au démarrage du run",
		historyTrigger(historyTriggerName))
	c.DestinationGrants = map[schema.Dialect][]string{
		schema.MySQL:    {"GRANT SELECT, INSERT, UPDATE, DELETE ON {db}.* TO {user}"},
		schema.Postgres: writeGrants[schema.Postgres],
	}
	c.ExpectRunError = map[schema.Dialect]string{schema.MySQL: "triggers of", schema.Postgres: "triggers of"}
	return c
}

// rightsDestinationTriggerForeignDefiner: the account may take the trigger out of the way,
// but not put it back: the trigger was created by another account, and naming another
// definer takes SET_USER_ID. The run would drop it, write everything, and fail to create
// it again.
func rightsDestinationTriggerForeignDefiner() *Case {
	c := triggerWritesSyncedTable("rights-destination-trigger-foreign-definer",
		//nolint:misspell // titre du rapport, rédigé en français
		"Trigger MySQL créé par un autre compte, que le compte ne peut pas recréer : arrêt au démarrage du run",
		historyTrigger(historyTriggerName))
	c.Dialects = mysqlOnly
	c.DestinationGrants = map[schema.Dialect][]string{schema.MySQL: writeGrants[schema.MySQL]}
	c.ExpectRunError = map[schema.Dialect]string{schema.MySQL: "cannot put back the trigger"}
	return c
}

func articlesToWrite() (tables []*schema.Table, seed func(p Params, emit Emitter)) {
	tables = []*schema.Table{{
		Name: "ARTICLE",
		Columns: []schema.Column{
			{Name: idColumn, Type: schema.Int64()},
			{Name: "libelle", Type: schema.Varchar(40)},
		},
		PrimaryKey: []string{idColumn},
	}}
	seed = func(p Params, emit Emitter) {
		for i := int64(1); i <= 10; i++ {
			emit.Row("ARTICLE", []any{i, "article"}, Kept())
		}
	}
	return tables, seed
}

func rightsDestinationReadOnlyAccount() *Case {
	tables, seed := articlesToWrite()
	return &Case{
		ID:       "rights-destination-read-only-account",
		Priority: P2,
		//nolint:misspell // titre du rapport, rédigé en français
		Title:  "Destination sans droit d'écriture : arrêt au démarrage du run sur le contrôle des droits",
		Tables: tables,
		DestinationGrants: map[schema.Dialect][]string{
			schema.MySQL: {"GRANT SELECT ON {db}.* TO {user}"},
			schema.Postgres: {
				"GRANT USAGE ON SCHEMA {db} TO {user}",
				"GRANT SELECT ON ALL TABLES IN SCHEMA {db} TO {user}",
			},
		},
		ExpectRunError: map[schema.Dialect]string{schema.MySQL: preflightStop, schema.Postgres: preflightStop},
		// The report the run keeps names what is missing on the table, as the run says it.
		ExpectFindings: []ExpectedFinding{
			expectFinding(mgmtv1alpha1.PreflightFinding_KIND_WRITABLE, findingBlocking, "ARTICLE"),
		},
		Seed: seed,
	}
}

// rightsDestinationCannotSuspendForeignKeys: the account may write every table but is not
// allowed to suspend foreign keys, which on PostgreSQL takes a superuser or GRANT SET ON
// PARAMETER session_replication_role. Athanor writes a table in one pass with the keys
// suspended, so it cannot run there and must say so before writing anything. Benthos does
// not suspend them: the same account is enough for it, and the check must not stop it.
func rightsDestinationCannotSuspendForeignKeys() *Case {
	tables, seed := articlesToWrite()
	return &Case{
		ID:       "rights-destination-cannot-suspend-foreign-keys",
		Dialects: postgresOnly,
		Priority: P2,
		//nolint:misspell // titre du rapport, rédigé en français
		Title:  "Destination sans le droit de suspendre les FK : Athanor s'arrête au démarrage, Benthos passe",
		Tables: tables,
		DestinationGrants: map[schema.Dialect][]string{
			schema.Postgres: {
				"GRANT USAGE ON SCHEMA {db} TO {user}",
				"GRANT SELECT, INSERT, UPDATE, DELETE, TRUNCATE ON ALL TABLES IN SCHEMA {db} TO {user}",
			},
		},
		ExpectRunError: map[schema.Dialect]string{schema.Postgres: "cannot suspend foreign keys"},
		FailingEngines: []string{"athanor"},
		Seed:           seed,
	}
}
