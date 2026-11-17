package sentrysql

import (
	"context"
	"database/sql/driver"
	"sync/atomic"

	"github.com/getsentry/sentry-go"
)

// sentryTx wraps a driver.Tx. It also holds the current active span of
// the transaction and the context carrying it. On Commit/Rollback the span is set to nil.
type sentryTx struct {
	tx   driver.Tx
	ctx  context.Context
	span atomic.Pointer[sentry.Span]
}

func newTx(ctx context.Context, tx driver.Tx, span *sentry.Span) *sentryTx {
	t := &sentryTx{tx: tx, ctx: ctx}
	t.span.Store(span)
	return t
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

// contextOrNil returns the context of an active transaction span or nil.
func (t *sentryTx) contextOrNil() context.Context {
	if t == nil || t.span.Load() == nil {
		return nil
	}
	return t.ctx
}

func (t *sentryTx) finish(err error, status sentry.SpanStatus) {
	if t == nil {
		return
	}
	span := t.span.Swap(nil)
	if span == nil {
		return
	}
	if err != nil {
		span.Status = sentry.SpanStatusInternalError
	} else {
		span.Status = status
	}
	span.Finish()
}
