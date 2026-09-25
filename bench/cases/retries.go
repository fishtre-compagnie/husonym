package cases

import (
	"fmt"

	mgmtv1alpha1 "github.com/fishtre-compagnie/husonym/backend/gen/go/protos/mgmt/v1alpha1"

	"github.com/fishtre-compagnie/husonym/bench/schema"
)

// retryCases exercise the second attempt of a table sync. Both engines then rewrite the
// page with "do nothing". On MySQL that was once INSERT IGNORE, which skips the rows
// already there and also downgrades every other error to a warning; the cases keep it
// from coming back.
func retryCases() []*Case {
	return []*Case{
		retryInsertIgnoreMasksTruncation(),
		retryKeylessTableDuplicates(),
		workerKilledMidPage(),
	}
}

// retryInsertIgnoreMasksTruncation: the destination column is narrower than the source
// one. The first attempt fails on the too long value, as it should; the retry, written with
// "do nothing", must fail the same way. MySQL's INSERT IGNORE truncated the values and
// completed; PostgreSQL's ON CONFLICT DO NOTHING is not expected to, which the case keeps.
// The only correct outcome is the failed run.
func retryInsertIgnoreMasksTruncation() *Case {
	return &Case{
		ID:       "retry-insert-ignore-masks-truncation",
		Priority: P1,
		Title:    "Nouvelle tentative en « do nothing » : la troncature refusée à la première tentative reste refusée",
		Tables: []*schema.Table{{
			Name: "ARTICLE",
			Columns: []schema.Column{
				{Name: idColumn, Type: schema.Int64()},
				{Name: "libelle", Type: schema.Varchar(40)},
			},
			PrimaryKey: []string{idColumn},
		}},
		DestinationSetupFor: map[schema.Dialect][]string{
			schema.MySQL:    {"ALTER TABLE {db}.`ARTICLE` MODIFY `libelle` VARCHAR(5) NOT NULL"},
			schema.Postgres: {"ALTER TABLE {db}.{q:ARTICLE} ALTER COLUMN {q:libelle} TYPE varchar(5)"},
		},
		Job: Job{SyncAttempts: 3},
		ExpectFindings: []ExpectedFinding{
			expectFinding(mgmtv1alpha1.PreflightFinding_KIND_OUTPUT_TOO_LONG, findingWarning, "ARTICLE"),
		},
		ExpectRunError: map[schema.Dialect]string{
			schema.MySQL:    "Data too long",
			schema.Postgres: "value too long for type character varying(5)",
		},
		Seed: func(p Params, emit Emitter) {
			for i := int64(1); i <= 20; i++ {
				emit.Row("ARTICLE", []any{i, fmt.Sprintf("libellé bien trop long %d", i)}, Kept())
			}
		},
	}
}

// retryKeylessTableDuplicates: a table without any key, and a write that fails once after
// its first batches are committed. The retry rewrites the table (a keyless table is read in
// one stream); "do nothing" has no key to collide on, so the rows already committed are
// written twice.
//
// The failure is injected by a CHECK constraint whose function fails the first insert of
// the marked row only: a sequence remembers it did, since nextval is not rolled back with
// the failed statement. A trigger would not do: the run takes the triggers of its tables
// out of its way. MySQL has no other hook on each row — a CHECK constraint, a generated
// column or a default cannot call a function of one's own — hence a case of PostgreSQL's:
// what it exercises, the retry of the engines, is the same on both databases.
func retryKeylessTableDuplicates() *Case {
	const marked = int64(999)
	return &Case{
		ID:       "retry-keyless-table-duplicates",
		Dialects: postgresOnly,
		Priority: P1,
		//nolint:misspell // titre du rapport, rédigé en français
		Title: "Table sans clé : une écriture reprise après un échec partiel est en double",
		Tables: []*schema.Table{{
			Name: "JOURNAL",
			Columns: []schema.Column{
				{Name: "niveau", Type: schema.Int32()},
				{Name: "message", Type: schema.Varchar(60)},
			},
		}},
		DestinationSetupFor: map[schema.Dialect][]string{schema.Postgres: {
			"CREATE SEQUENCE {db}.{q:bench_fault}",
			"CREATE FUNCTION {db}.{q:bench_fault_once}(niveau integer) RETURNS boolean LANGUAGE plpgsql IMMUTABLE AS $$ " +
				fmt.Sprintf("BEGIN IF niveau = %d AND nextval('{db}.{q:bench_fault}') = 1 THEN ", marked) +
				"RAISE EXCEPTION 'bench: injected failure'; END IF; RETURN true; END $$",
			"ALTER TABLE {db}.{q:JOURNAL} ADD CONSTRAINT {q:bench_fault} CHECK ({db}.{q:bench_fault_once}({q:niveau}))",
		}},
		// Batches smaller than the table, so that some are committed before the failure.
		Job: Job{SyncAttempts: 3, BatchCount: 10},
		ExpectFindings: []ExpectedFinding{
			expectFinding(mgmtv1alpha1.PreflightFinding_KIND_READ_IN_ONE_STREAM, findingInformation, "JOURNAL"),
			expectFinding(mgmtv1alpha1.PreflightFinding_KIND_RETRY_MAY_DUPLICATE, findingWarning, "JOURNAL").on("benthos"),
		},
		Seed: func(p Params, emit Emitter) {
			for i := 1; i < p.PageLimit; i++ {
				emit.Row("JOURNAL", []any{int64(1), fmt.Sprintf("m%07d", i)}, Kept())
			}
			emit.Row("JOURNAL", []any{marked, "zz ligne marquée"}, Kept())
		},
	}
}

// workerKilledMidPage: the worker is killed while a page is half written — some write
// batches committed, the next one waiting. Temporal gives the page to the restarted worker,
// which writes it again: every row must be there once, none lost, none twice, and the
// row of the bench that held the page up never committed.
func workerKilledMidPage() *Case {
	return &Case{
		ID:       "worker-killed-mid-page",
		Priority: P1,
		Title:    "Worker tué en cours de page : la page reprise n'a ni ligne perdue ni ligne en double",
		Tables: []*schema.Table{{
			Name: "ARTICLE",
			Columns: []schema.Column{
				{Name: idColumn, Type: schema.Int64()},
				{Name: "libelle", Type: schema.Varchar(40)},
			},
			PrimaryKey: []string{idColumn},
		}},
		KillWorker: &WorkerKill{Table: "ARTICLE", BlockingRow: []any{int64(150), "verrou du banc"}},
		// Batches smaller than a page, so that part of the page is committed when it waits.
		Job: Job{SyncAttempts: 3, BatchCount: 10},
		Seed: func(p Params, emit Emitter) {
			for i := int64(1); i <= int64(2*p.PageLimit+p.PageLimit/2); i++ {
				emit.Row("ARTICLE", []any{i, fmt.Sprintf("article %d", i)}, Kept())
			}
		},
	}
}
