package sentrysql

import (
	"database/sql/driver"
	"sync"

	"github.com/getsentry/sentry-go"
)

// sentryTx wraps a driver.Tx.
type sentryTx struct {
	tx   driver.Tx
	conn *sentryConn
	span *sentry.Span
	once sync.Once
}

// Commit implements driver.Tx.
func (t *sentryTx) Commit() error {
	err := t.tx.Commit()
	t.finish(err, sentry.SpanStatusOK)
	return err
}

// Rollback implements driver.Tx.
func (t *sentryTx) Rollback() error {
	err := t.tx.Rollback()
	t.finish(err, sentry.SpanStatusAborted)
	return err
}

func (t *sentryTx) finish(err error, status sentry.SpanStatus) {
	if t == nil {
		return
	}
	t.once.Do(func() {
		if t.conn != nil {
			t.conn.setTxSpan(nil)
		}
		if t.span == nil {
			return
		}
		if err != nil {
			t.span.Status = sentry.SpanStatusInternalError
		} else {
			t.span.Status = status
		}
		t.span.Finish()
	})
}
