package sentrysql

import "database/sql/driver"

// sentryTx wraps a driver.Tx.
type sentryTx struct {
	tx driver.Tx
}

// Commit implements driver.Tx.
func (t *sentryTx) Commit() error { return t.tx.Commit() }

// Rollback implements driver.Tx.
func (t *sentryTx) Rollback() error { return t.tx.Rollback() }
