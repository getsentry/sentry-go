package sentrysql_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"testing"

	sentrysql "github.com/getsentry/sentry-go/sql"
	"github.com/getsentry/sentry-go/sql/internal/fakedriver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpen_ErrorsWhenSystemUnrecognized(t *testing.T) {
	t.Parallel()
	fakedriver.Register("fake-ctx-open-req", fakedriver.NewCtx())
	_, err := sentrysql.Open("fake-ctx-open-req", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unable to autodetect db.system")
	assert.Contains(t, err.Error(), "fake-ctx-open-req")
}

func TestOpen_AutodetectsSystemFromDriverName(t *testing.T) {
	t.Parallel()
	fakedriver.Register("mysql", fakedriver.NewCtx())

	db, err := sentrysql.Open("mysql", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.ExecContext(context.Background(), "INSERT INTO t VALUES (1)")
	require.NoError(t, err)
}

func TestOpenDB_RequiresDatabaseSystem(t *testing.T) {
	t.Parallel()
	conn := fakedriver.NewCtxConnector(fakedriver.NewCtx())
	_, err := sentrysql.OpenDB(conn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "WithDatabaseSystem is required")
}

func TestWrapDriver_RequiresDatabaseSystem(t *testing.T) {
	t.Parallel()
	_, err := sentrysql.WrapDriver(fakedriver.NewCtx())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "WithDatabaseSystem is required")
}

func TestWrapConnector_RequiresDatabaseSystem(t *testing.T) {
	t.Parallel()
	conn := fakedriver.NewCtxConnector(fakedriver.NewCtx())
	_, err := sentrysql.WrapConnector(conn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "WithDatabaseSystem is required")
}

func TestRegister_AutodetectsSystemFromName(t *testing.T) {
	t.Parallel()
	err := sentrysql.Register("postgres", fakedriver.NewCtx())
	require.NoError(t, err)

	db, err := sql.Open("sentrysql-postgres", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.ExecContext(context.Background(), "SELECT 1")
	require.NoError(t, err)
}

func TestOpen_PassthroughContextDriver(t *testing.T) {
	t.Parallel()
	fakedriver.Register("fake-ctx-open", fakedriver.NewCtx())
	db, err := sentrysql.Open("fake-ctx-open", "",
		sentrysql.WithDatabaseSystem(sentrysql.SystemPostgreSQL),
		sentrysql.WithDatabaseName("appdb"),
		sentrysql.WithServerAddress("localhost", "5432"),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	require.NoError(t, db.PingContext(ctx))

	_, err = db.ExecContext(ctx, "INSERT INTO t VALUES (1)")
	require.NoError(t, err)

	rows, err := db.QueryContext(ctx, "SELECT 1")
	require.NoError(t, err)
	_ = rows.Close()
}

func TestOpen_PassthroughLegacyDriver(t *testing.T) {
	t.Parallel()
	fakedriver.Register("fake-legacy-open", fakedriver.NewLegacy())
	db, err := sentrysql.Open("fake-legacy-open", "",
		sentrysql.WithDatabaseSystem("mysql"),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.ExecContext(context.Background(), "INSERT INTO t VALUES (1)")
	require.NoError(t, err)
	_, err = db.QueryContext(context.Background(), "SELECT 1")
	require.NoError(t, err)
}

func TestOpen_PropagatesDriverError(t *testing.T) {
	t.Parallel()
	drv := fakedriver.NewCtx()
	drv.SetFailure(fakedriver.ErrDriver)
	fakedriver.Register("fake-ctx-err", drv)
	db, err := sentrysql.Open("fake-ctx-err", "", sentrysql.WithDatabaseSystem("postgresql"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.ExecContext(context.Background(), "INSERT INTO t VALUES (1)")
	require.ErrorIs(t, err, fakedriver.ErrDriver)
}

func TestRegister_WrappedDriverUsable(t *testing.T) {
	t.Parallel()
	err := sentrysql.Register("reg-ctx",
		fakedriver.NewCtx(),
		sentrysql.WithDatabaseSystem("postgresql"),
	)
	require.NoError(t, err)

	db, err := sql.Open("sentrysql-reg-ctx", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.ExecContext(context.Background(), "SELECT 1")
	require.NoError(t, err)
}

func TestRegister_ErrorsWhenSystemUnrecognized(t *testing.T) {
	t.Parallel()
	// Unknown driver name and no explicit WithDatabaseSystem — Register must
	// error with an autodetect-miss message.
	err := sentrysql.Register("reg-missing", fakedriver.NewCtx())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unable to autodetect db.system")
}

func TestOpen_PassthroughMinimalDriver(t *testing.T) {
	t.Parallel()
	fakedriver.Register("fake-minimal-open", fakedriver.NewMinimal())
	db, err := sentrysql.Open("fake-minimal-open", "",
		sentrysql.WithDatabaseSystem("sqlite"),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()
	_, err = db.ExecContext(ctx, "INSERT INTO t VALUES (1)")
	require.NoError(t, err)

	rows, err := db.QueryContext(ctx, "SELECT 1")
	require.NoError(t, err)
	_ = rows.Close()
}

func TestOpen_MinimalDriverPropagatesStmtError(t *testing.T) {
	t.Parallel()
	drv := fakedriver.NewMinimal()
	drv.SetFailure(fakedriver.ErrDriver)
	fakedriver.Register("fake-minimal-err", drv)
	db, err := sentrysql.Open("fake-minimal-err", "", sentrysql.WithDatabaseSystem("sqlite"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.ExecContext(context.Background(), "INSERT INTO t VALUES (1)")
	require.ErrorIs(t, err, fakedriver.ErrDriver)
}

func TestBeginTx_RejectsNonDefaultOptsOnLegacyDriver(t *testing.T) {
	t.Parallel()
	fakedriver.Register("fake-legacy-begintx", fakedriver.NewLegacy())
	db, err := sentrysql.Open("fake-legacy-begintx", "",
		sentrysql.WithDatabaseSystem("mysql"),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	ctx := context.Background()

	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())

	_, err = db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-default TxOptions")

	_, err = db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-default TxOptions")
}

func TestOpenDB_PropagatesConnectorClose(t *testing.T) {
	t.Parallel()
	conn := fakedriver.NewCtxConnector(fakedriver.NewCtx())
	db, err := sentrysql.OpenDB(conn, sentrysql.WithDatabaseSystem("postgresql"))
	require.NoError(t, err)

	require.NoError(t, db.PingContext(context.Background()))
	require.NoError(t, db.Close())

	assert.Equal(t, 1, conn.CloseCount(),
		"sentryConnector.Close must delegate to the inner io.Closer")
}

func TestWrapConnector_LegacyDriverHidesDriverContext(t *testing.T) {
	t.Parallel()
	conn := fakedriver.NewLegacyConnector(fakedriver.NewLegacy())
	db, err := sentrysql.OpenDB(conn, sentrysql.WithDatabaseSystem("mysql"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, ok := db.Driver().(driver.DriverContext)
	assert.False(t, ok, "wrapper must not claim DriverContext when inner driver lacks it")
}

func TestWrapConnector_CtxDriverExposesDriverContext(t *testing.T) {
	t.Parallel()
	conn := fakedriver.NewCtxConnector(fakedriver.NewCtx())
	db, err := sentrysql.OpenDB(conn, sentrysql.WithDatabaseSystem("postgresql"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, ok := db.Driver().(driver.DriverContext)
	assert.True(t, ok, "wrapper must expose DriverContext when inner driver implements it")
}

func TestOpenDB_WrapsConnector(t *testing.T) {
	t.Parallel()
	connector, err := fakedriver.NewCtx().OpenConnector("")
	require.NoError(t, err)
	db, err := sentrysql.OpenDB(connector, sentrysql.WithDatabaseSystem("postgresql"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.ExecContext(context.Background(), "SELECT 1")
	require.NoError(t, err)
}
