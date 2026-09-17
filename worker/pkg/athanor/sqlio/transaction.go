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
// references. The setting belongs to the connection, which returns to the pool after the
// transaction: checks are always turned back on first, even when the write fails.
func InTransaction(
	ctx context.Context,
	db TxBeginner,
	dialect Dialect,
	disableForeignKeyChecks bool,
	write func(Tx) error,
) (err error) {
	var disable, enable string
	if disableForeignKeyChecks {
		var ok bool
		if disable, enable, ok = dialect.ForeignKeyChecksStatements(); !ok {
			return fmt.Errorf("sqlio: %s ne permet pas de désactiver les clés étrangères", dialect.Driver())
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlio: ouverture de transaction: %w", err)
	}
	if disable != "" {
		if _, err := tx.ExecContext(ctx, disable); err != nil {
			return errors.Join(fmt.Errorf("sqlio: désactivation des clés étrangères: %w", err), tx.Rollback())
		}
	}
	defer func() {
		if enable != "" {
			if _, rerr := tx.ExecContext(ctx, enable); rerr != nil {
				err = errors.Join(err, fmt.Errorf("sqlio: réactivation des clés étrangères: %w", rerr))
			}
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
