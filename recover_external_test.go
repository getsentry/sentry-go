package sentry_test

import (
	"context"
	"errors"
	"testing"

	"github.com/getsentry/sentry-go"
	pkgErrors "github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
func recoverPanic(ctx context.Context, panicFunc func()) {
	defer func() {
		if recovered := recover(); recovered != nil {
			sentry.Recover(ctx, recovered)
		}
	}()

	panicFunc()
}

//go:noinline
func recoverHandledError(ctx context.Context) {
	sentry.Recover(ctx, errors.New("boom"))
}

func newRecoverTestContext(t *testing.T) (context.Context, *sentry.MockTransport) {
	t.Helper()

	transport := &sentry.MockTransport{}
	client, err := sentry.NewClient(sentry.ClientOptions{
		Transport:        transport,
		AttachStacktrace: true,
		Integrations: func([]sentry.Integration) []sentry.Integration {
			return nil
		},
	})
	require.NoError(t, err)
	t.Cleanup(client.Close)

	ctx := sentry.ContextWithClient(
		sentry.ContextWithScope(context.Background(), sentry.NewScope()),
		client,
	)
	return ctx, transport
}

func requireTopFrame(t *testing.T, stacktrace *sentry.Stacktrace, function string) {
	t.Helper()
	require.NotNil(t, stacktrace)
	require.NotEmpty(t, stacktrace.Frames)
	assert.Equal(t, function, stacktrace.Frames[len(stacktrace.Frames)-1].Function)
}

func TestRecoverUsesPanicOriginStacktrace(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		panicFunc      func()
		topFrame       string
		preservesStack bool
	}{
		{
			name:      "error",
			panicFunc: panicWithError,
			topFrame:  "panicWithError",
		},
		{
			name:      "runtime error",
			panicFunc: panicWithNilPointer,
			topFrame:  "panicWithNilPointer",
		},
		{
			name:           "error with stacktrace",
			panicFunc:      panicWithErrorStacktrace,
			topFrame:       "newErrorWithStacktrace",
			preservesStack: true,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ctx, transport := newRecoverTestContext(t)
			recoverPanic(ctx, test.panicFunc)

			events := transport.Events()
			require.Len(t, events, 1)
			require.NotEmpty(t, events[0].Exception)
			exception := events[0].Exception[len(events[0].Exception)-1]
			requireTopFrame(t, exception.Stacktrace, test.topFrame)

			if test.preservesStack {
				assert.NotEqual(t, "panicWithErrorStacktrace", exception.Stacktrace.Frames[len(exception.Stacktrace.Frames)-1].Function)
			}
		})
	}
}

func TestRecoverValueUsesPanicOriginStacktraceWhenConfigured(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		panicFunc func()
		topFrame  string
	}{
		{
			name:      "string",
			panicFunc: panicWithString,
			topFrame:  "panicWithString",
		},
		{
			name:      "arbitrary value",
			panicFunc: panicWithArbitraryValue,
			topFrame:  "panicWithArbitraryValue",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			ctx, transport := newRecoverTestContext(t)
			recoverPanic(ctx, test.panicFunc)

			events := transport.Events()
			require.Len(t, events, 1)
			require.Len(t, events[0].Threads, 1)
			requireTopFrame(t, events[0].Threads[0].Stacktrace, test.topFrame)
		})
	}
}

func TestRecoverOutsidePanicKeepsCallerStacktrace(t *testing.T) {
	t.Parallel()

	ctx, transport := newRecoverTestContext(t)
	recoverHandledError(ctx)

	events := transport.Events()
	require.Len(t, events, 1)
	require.Len(t, events[0].Exception, 1)
	requireTopFrame(t, events[0].Exception[0].Stacktrace, "recoverHandledError")
}
