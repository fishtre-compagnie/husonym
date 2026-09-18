package cases

import (
	"fmt"

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
	}
}

// retryInsertIgnoreMasksTruncation: the destination column is narrower than the source
// one. The first attempt fails on "Data too long", as it should; the retry, written with
// INSERT IGNORE, truncates the values and completes. The only correct outcome is the
// failed run.
func retryInsertIgnoreMasksTruncation() *Case {
	return &Case{
		ID:       "retry-insert-ignore-masks-truncation",
		Dialects: mysqlOnly,
		Priority: P1,
		Title:    "Nouvelle tentative en INSERT IGNORE : la troncature refusée à la première tentative passe en silence",
		Tables: []*schema.Table{{
			Name: "ARTICLE",
			Columns: []schema.Column{
				{Name: idColumn, Type: schema.Int64()},
				{Name: "libelle", Type: schema.Varchar(40)},
			},
			PrimaryKey: []string{idColumn},
		}},
		DestinationSetup: []string{"ALTER TABLE {db}.`ARTICLE` MODIFY `libelle` VARCHAR(5) NOT NULL"},
		Job:              Job{SyncAttempts: 3},
		ExpectRunError:   map[schema.Dialect]string{schema.MySQL: "Data too long"},
		Seed: func(p Params, emit Emitter) {
			for i := int64(1); i <= 20; i++ {
				emit.Row("ARTICLE", []any{i, fmt.Sprintf("libellé bien trop long %d", i)}, Kept())
			}
		},
	}
}

// retryKeylessTableDuplicates: a table without any key, and a page that fails once after
// its first write batch is committed. The retry rewrites the whole page; "do nothing" has
// no key to collide on, so the rows of the first batch are written twice. A trigger on the
// destination fails the first insert of the marked row only, remembering it did in a
// MyISAM table, which keeps its row when the failed statement rolls back.
func retryKeylessTableDuplicates() *Case {
	const marked = int64(999)
	return &Case{
		ID:       "retry-keyless-table-duplicates",
		Dialects: mysqlOnly,
		Priority: P1,
		//nolint:misspell // titre du rapport, rédigé en français
		Title:        "Table sans clé : une page réécrite après un échec partiel est en double",
		MinPageLimit: 2500,
		Tables: []*schema.Table{{
			Name: "JOURNAL",
			Columns: []schema.Column{
				{Name: "niveau", Type: schema.Int32()},
				{Name: "message", Type: schema.Varchar(60)},
			},
		}},
		DestinationSetup: []string{
			"CREATE TABLE {db}.`bench_fault` (`n` INT) ENGINE=MyISAM",
			"CREATE TRIGGER {db}.`trg_bench_fault` BEFORE INSERT ON {db}.`JOURNAL` FOR EACH ROW BEGIN " +
				fmt.Sprintf("IF NEW.`niveau` = %d AND (SELECT COUNT(*) FROM {db}.`bench_fault`) = 0 THEN ", marked) +
				"INSERT INTO {db}.`bench_fault` VALUES (1); " +
				"SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'bench: injected failure'; " +
				"END IF; END",
		},
		Job: Job{SyncAttempts: 3},
		Seed: func(p Params, emit Emitter) {
			// The marked row sorts last of the page (order falls back to message, niveau).
			for i := 1; i < p.PageLimit; i++ {
				emit.Row("JOURNAL", []any{int64(1), fmt.Sprintf("m%07d", i)}, Kept())
			}
			emit.Row("JOURNAL", []any{marked, "zz ligne marquée"}, Kept())
		},
	}
}
