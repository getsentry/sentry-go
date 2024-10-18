package sentrysql

import (
	"context"
	"database/sql/driver"
)

type sentryTx struct {
	originalTx driver.Tx
	ctx        context.Context
	config     *sentrySqlConfig
}

// Commit implements driver.Tx.
func (s *sentryTx) Commit() error {
	return s.originalTx.Commit()
}

// Rollback implements driver.Tx.
func (s *sentryTx) Rollback() error {
	return s.originalTx.Rollback()
}
