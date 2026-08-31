package sentry_test

import (
	"context"
	"errors"
	"testing"

	pkgErrors "github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/getsentry/sentry-go"
)

//go:noinline
func panicWithError() {
	panic(errors.New("boom"))
}

//go:noinline
func panicWithNilPointer() {
	var pointer *int
	*pointer = 1
}

//go:noinline
func newErrorWithStacktrace() error {
	return pkgErrors.New("boom")
}

//go:noinline
func panicWithErrorStacktrace() {
	panic(newErrorWithStacktrace())
}

//go:noinline
func panicWithString() {
	panic("boom")
}

//go:noinline
func panicWithArbitraryValue() {
	panic(42)
}

//go:noinline
func recoverPanic(client *sentry.Client, panicFunc func()) {
	ctx, scope := sentry.WithIsolationScope(context.Background())
	scope.SetClient(client)
	defer sentry.Recover(ctx)
	panicFunc()
}

//go:noinline
func capturePanic(client *sentry.Client, panicFunc func()) {
	ctx, scope := sentry.WithIsolationScope(context.Background())
	scope.SetClient(client)
	defer func() {
		sentry.CapturePanic(ctx, recover())
	}()
	panicFunc()
}

func requirePanicStacktrace(t *testing.T, stacktrace *sentry.Stacktrace, topFrame string) {
	t.Helper()
	require.NotNil(t, stacktrace)
	require.NotEmpty(t, stacktrace.Frames)
	assert.Equal(t, topFrame, stacktrace.Frames[len(stacktrace.Frames)-1].Function)
	for _, frame := range stacktrace.Frames {
		assert.NotEqual(t, "github.com/getsentry/sentry-go", frame.Module)
	}
}

func testPanicCapture(t *testing.T, capture func(*sentry.Client, func())) {
	t.Helper()

	tests := []struct {
		name      string
		panicFunc func()
		topFrame  string
		exception bool
	}{
		{name: "error", panicFunc: panicWithError, topFrame: "panicWithError", exception: true},
		{name: "runtime error", panicFunc: panicWithNilPointer, topFrame: "panicWithNilPointer", exception: true},
		{name: "error with stacktrace", panicFunc: panicWithErrorStacktrace, topFrame: "panicWithErrorStacktrace", exception: true},
		{name: "string", panicFunc: panicWithString, topFrame: "panicWithString"},
		{name: "arbitrary value", panicFunc: panicWithArbitraryValue, topFrame: "panicWithArbitraryValue"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			transport := &sentry.MockTransport{}
			client, err := sentry.NewClient(sentry.ClientOptions{
				Transport: transport,
				Integrations: func([]sentry.Integration) []sentry.Integration {
					return nil
				},
			})
			require.NoError(t, err)
			capture(client, test.panicFunc)

			events := transport.Events()
			require.Len(t, events, 1)

			var stacktrace *sentry.Stacktrace
			if test.exception {
				require.NotEmpty(t, events[0].Exception)
				stacktrace = events[0].Exception[len(events[0].Exception)-1].Stacktrace
			} else {
				require.Len(t, events[0].Threads, 1)
				stacktrace = events[0].Threads[0].Stacktrace
			}
			requirePanicStacktrace(t, stacktrace, test.topFrame)
		})
	}
}

func TestRecoverUsesPanicOriginStacktrace(t *testing.T) {
	t.Parallel()
	testPanicCapture(t, recoverPanic)
}

func TestCapturePanicUsesPanicOriginStacktrace(t *testing.T) {
	t.Parallel()
	testPanicCapture(t, capturePanic)
}
