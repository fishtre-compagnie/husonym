package sqlio

import (
	"context"
	"errors"
	"fmt"
)

// InTransaction runs write against one transaction of the destination, so that what it
// writes — a page of a table — is there whole or not at all. A page that failed half way
// leaves nothing behind: its retry cannot write rows twice, which matters for tables
// without any key, where "do nothing" has no conflict to detect.
//
// With disableForeignKeyChecks, foreign key checks are off for the transaction, so a
// table is written in one pass whatever the order of its rows and of the tables it
// references. The session is also set to store values as they are (see
// Dialect.FaithfulWriteStatements). These settings belong to the connection, which
// returns to the pool after the transaction: they are always undone first, even when the
// write fails.
func InTransaction(
	ctx context.Context,
	db TxBeginner,
	dialect Dialect,
	disableForeignKeyChecks bool,
	write func(Tx) error,
) (err error) {
	begin, end := dialect.FaithfulWriteStatements()
	if disableForeignKeyChecks {
		disable, enable, ok := dialect.ForeignKeyChecksStatements()
		if !ok {
			return fmt.Errorf("sqlio: %s ne permet pas de désactiver les clés étrangères", dialect.Driver())
		}
		begin = append([]string{disable}, begin...)
		// A setting bound to the transaction goes away with it: there is nothing to undo,
		// and the dialect says so with an empty statement.
		if enable != "" {
			end = append(end, enable)
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlio: ouverture de transaction: %w", err)
	}
	for _, stmt := range begin {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return errors.Join(fmt.Errorf("sqlio: réglage de la session d'écriture (%s): %w", stmt, err), tx.Rollback())
		}
	}
	defer func() {
		restored := true
		for _, stmt := range end {
			if _, rerr := tx.ExecContext(ctx, stmt); rerr != nil {
				restored = false
				err = errors.Join(err, fmt.Errorf("sqlio: rétablissement de la session d'écriture (%s): %w", stmt, rerr))
			}
		}
		if !restored {
			discardSession(ctx, tx, dialect)
		}
		if err != nil {
			err = errors.Join(err, tx.Rollback())
			return
		}
		if cerr := tx.Commit(); cerr != nil {
			err = fmt.Errorf("sqlio: validation de la transaction: %w", cerr)
		}
	}()
	return write(tx)
}

// discardSession has the database drop the connection the page was written on, after its
// settings could not be restored. Rolling back does not undo a session setting, and the
// pool hands the connection to the next table: it would be written with foreign key checks
// off and a lax sql_mode, and nothing would say so. The statement kills the connection it
// runs on, so its own failure is expected and the context of the run, canceled or not,
// must not stop it.
func discardSession(ctx context.Context, tx Execer, dialect Dialect) {
	stmt, ok := dialect.DiscardSessionStatement()
	if !ok {
		return
	}
	_, _ = tx.ExecContext(context.WithoutCancel(ctx), stmt)
}
