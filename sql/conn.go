package sentrysql

import (
	"context"
	"database/sql/driver"
	"errors"
	"sync"

	"github.com/getsentry/sentry-go"
)

// sentryConn wraps a driver.Conn.
type sentryConn struct {
	mu     sync.RWMutex
	cfg    *config
	conn   driver.Conn
	txSpan *sentry.Span
}

func newConn(c driver.Conn, cfg *config) driver.Conn {
	return &sentryConn{conn: c, cfg: cfg}
}

// Ping implements driver.Pinger when the underlying connection does.
func (c *sentryConn) Ping(ctx context.Context) error {
	if p, ok := c.conn.(driver.Pinger); ok {
		return p.Ping(ctx)
	}
	return nil
}

// QueryContext implements driver.QueryerContext with fallback to the legacy
// driver.Queryer path.
//
// nolint: dupl // we don't want to use a helper for Query/Exec Context.
func (c *sentryConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (rows driver.Rows, err error) {
	if qc, ok := c.conn.(driver.QueryerContext); ok {
		span := startQuerySpan(ctx, c, c.cfg, opQuery, query)
		defer func() { finishSpan(span, err) }()
		return qc.QueryContext(ctx, query, args)
	}
	qr, ok := c.conn.(driver.Queryer) //nolint:staticcheck // legacy driver.Queryer fallback is intentional.
	if !ok {
		return nil, driver.ErrSkip
	}
	values, cerr := namedValuesToValues(args)
	if cerr != nil {
		return nil, cerr
	}
	select {
	default:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	span := startQuerySpan(ctx, c, c.cfg, opQuery, query)
	defer func() { finishSpan(span, err) }()
	return qr.Query(query, values)
}

// ExecContext implements driver.ExecerContext with fallback to the legacy
// driver.Execer path.
//
// nolint: dupl // we don't want to use a helper for Query/Exec Context.
func (c *sentryConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (res driver.Result, err error) {
	if ec, ok := c.conn.(driver.ExecerContext); ok {
		span := startQuerySpan(ctx, c, c.cfg, opExec, query)
		defer func() { finishSpan(span, err) }()
		return ec.ExecContext(ctx, query, args)
	}
	ex, ok := c.conn.(driver.Execer) //nolint:staticcheck // legacy driver.Execer fallback is intentional.
	if !ok {
		return nil, driver.ErrSkip
	}
	values, cerr := namedValuesToValues(args)
	if cerr != nil {
		return nil, cerr
	}
	select {
	default:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	span := startQuerySpan(ctx, c, c.cfg, opExec, query)
	defer func() { finishSpan(span, err) }()
	return ex.Exec(query, values)
}

// PrepareContext implements driver.ConnPrepareContext with fallback to
// Prepare when the underlying connection does not support it.
func (c *sentryConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if cp, ok := c.conn.(driver.ConnPrepareContext); ok {
		stmt, err := cp.PrepareContext(ctx, query)
		if err != nil {
			return nil, err
		}
		return newStmt(stmt, c, query), nil
	}
	stmt, err := c.Prepare(query)
	if err != nil {
		return nil, err
	}
	select {
	default:
	case <-ctx.Done():
		return nil, errors.Join(ctx.Err(), stmt.Close())
	}
	return stmt, nil
}

// Prepare implements driver.Conn.
func (c *sentryConn) Prepare(query string) (driver.Stmt, error) {
	stmt, err := c.conn.Prepare(query)
	if err != nil {
		return nil, err
	}
	return newStmt(stmt, c, query), nil
}

// Close implements driver.Conn.
func (c *sentryConn) Close() error { return c.conn.Close() }

// Begin implements driver.Conn.
func (c *sentryConn) Begin() (driver.Tx, error) {
	tx, err := c.conn.Begin() //nolint:staticcheck // required by driver.Conn; BeginTx covers the modern path.
	if err != nil {
		return nil, err
	}
	c.setTxSpan(nil)
	return &sentryTx{tx: tx, conn: c}, nil
}

// BeginTx implements driver.ConnBeginTx with fallback to Begin.
func (c *sentryConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	var (
		tx  driver.Tx
		err error
	)

	if cb, ok := c.conn.(driver.ConnBeginTx); ok {
		tx, err = cb.BeginTx(ctx, opts)
		if err != nil {
			return nil, err
		}
	} else {
		// Mirror stdlib ctxDriverBegin: reject non-default TxOptions that can't be
		// expressed through the legacy Begin().
		if opts.Isolation != 0 || opts.ReadOnly {
			return nil, errors.New("sentrysql: driver does not support non-default TxOptions")
		}
		tx, err = c.conn.Begin() //nolint:staticcheck // required for legacy BeginTx fallback.
		if err != nil {
			return nil, err
		}
		select {
		default:
		case <-ctx.Done():
			return nil, errors.Join(ctx.Err(), tx.Rollback())
		}
	}

	span := startTxSpan(ctx, c.cfg)
	c.setTxSpan(span)
	return &sentryTx{tx: tx, conn: c, span: span}, nil
}

// ResetSession implements driver.SessionResetter.
func (c *sentryConn) ResetSession(ctx context.Context) error {
	if r, ok := c.conn.(driver.SessionResetter); ok {
		return r.ResetSession(ctx)
	}
	return nil
}

// IsValid implements driver.Validator.
func (c *sentryConn) IsValid() bool {
	if v, ok := c.conn.(driver.Validator); ok {
		return v.IsValid()
	}
	return true
}

// CheckNamedValue implements driver.NamedValueChecker when the underlying
// connection supports it; otherwise it returns driver.ErrSkip so the standard
// library falls back to default value conversion.
func (c *sentryConn) CheckNamedValue(nv *driver.NamedValue) error {
	if ch, ok := c.conn.(driver.NamedValueChecker); ok {
		return ch.CheckNamedValue(nv)
	}
	return driver.ErrSkip
}

// Raw returns the underlying driver connection. Useful for type-assertions.
func (c *sentryConn) Raw() driver.Conn {
	return c.conn
}

// txSpanOrNil returns the parent transaction span set in the connection.
// If it doesn't exist, this returns a nil span.
func (c *sentryConn) txSpanOrNil() *sentry.Span {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.txSpan
}

// setTxSpan sets a span in the connection. This is meant to be set on BeginTx
// and removed on Commit/Rollback.
func (c *sentryConn) setTxSpan(span *sentry.Span) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.txSpan = span
}

// namedValuesToValues converts []driver.NamedValue to []driver.Value for
// fallback calls to the legacy driver.Execer and driver.Queryer interfaces.
func namedValuesToValues(named []driver.NamedValue) ([]driver.Value, error) {
	out := make([]driver.Value, len(named))
	for i, nv := range named {
		if nv.Name != "" {
			return nil, errors.New("sql: driver does not support named arguments")
		}
		out[i] = nv.Value
	}
	return out, nil
}
