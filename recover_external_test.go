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
func recoverPanic(client sentry.Client, panicFunc func()) {
	defer func() {
		if err := recover(); err != nil {
			client.Recover(context.Background(), err)
		}
	}()

	panicFunc()
}

//go:noinline
func recoverHandledError(client sentry.Client) {
	client.Recover(context.Background(), errors.New("boom"))
}

func newRecoverTestClient(t *testing.T) (sentry.Client, *sentry.MockTransport) {
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

	return client, transport
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

			client, transport := newRecoverTestClient(t)
			recoverPanic(client, test.panicFunc)

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

func TestRecoverValueAlwaysUsesPanicOriginStacktrace(t *testing.T) {
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

			client, transport := newRecoverTestClient(t)
			recoverPanic(client, test.panicFunc)

			events := transport.Events()
			require.Len(t, events, 1)
			require.Len(t, events[0].Threads, 1)
			requireTopFrame(t, events[0].Threads[0].Stacktrace, test.topFrame)
		})
	}
}

func TestRecoverOutsidePanicKeepsCallerStacktrace(t *testing.T) {
	t.Parallel()

	client, transport := newRecoverTestClient(t)
	recoverHandledError(client)

	events := transport.Events()
	require.Len(t, events, 1)
	require.Len(t, events[0].Exception, 1)
	requireTopFrame(t, events[0].Exception[0].Stacktrace, "recoverHandledError")
}
