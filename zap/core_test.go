package sentryzap

import (
	"errors"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/assert"
)

type customError struct {
	message string
}

func (e *customError) Error() string {
	return e.message
}

func (e *customError) Unwrap() error {
	return errors.New("unwrapped error")
}

type nestedError struct {
	message string
	cause   error
}

func (e *nestedError) Error() string {
	return e.message
}

func (e *nestedError) Unwrap() error {
	return e.cause
}

func TestAddExceptionsFromError(t *testing.T) {
	tests := map[string]struct {
		initialExceptions  []sentry.Exception
		initialProcessed   map[string]struct{}
		err                error
		disableStacktrace  bool
		expectedExceptions []sentry.Exception
		expectedProcessed  map[string]struct{}
	}{
		"Single error with stacktrace": {
			initialExceptions: []sentry.Exception{},
			initialProcessed:  map[string]struct{}{},
			err:               &customError{message: "test error"},
			disableStacktrace: false,
			expectedExceptions: []sentry.Exception{
				{Value: "test error", Type: "*sentryzap.customError", Stacktrace: &sentry.Stacktrace{}},
			},
			expectedProcessed: map[string]struct{}{
				"test error:*sentryzap.customError": {},
			},
		},
		"Nested errors with stacktrace": {
			initialExceptions: []sentry.Exception{},
			initialProcessed:  map[string]struct{}{},
			err: &nestedError{
				message: "outer error",
				cause:   &customError{message: "inner error"},
			},
			disableStacktrace: false,
			expectedExceptions: []sentry.Exception{
				{Value: "outer error", Type: "*sentryzap.nestedError", Stacktrace: &sentry.Stacktrace{}},
				{Value: "inner error", Type: "*sentryzap.customError", Stacktrace: &sentry.Stacktrace{}},
			},
			expectedProcessed: map[string]struct{}{
				"outer error:*sentryzap.nestedError": {},
				"inner error:*sentryzap.customError": {},
			},
		},
		"Duplicate error, skips processing": {
			initialExceptions: []sentry.Exception{},
			initialProcessed: map[string]struct{}{
				"test error:*sentryzap.customError": {},
			},
			err:                &customError{message: "test error"},
			disableStacktrace:  false,
			expectedExceptions: []sentry.Exception{},
			expectedProcessed: map[string]struct{}{
				"test error:*sentryzap.customError": {},
			},
		},
		"Error with disabled stacktrace": {
			initialExceptions: []sentry.Exception{},
			initialProcessed:  map[string]struct{}{},
			err:               &customError{message: "test error"},
			disableStacktrace: true,
			expectedExceptions: []sentry.Exception{
				{Value: "test error", Type: "*sentryzap.customError", Stacktrace: nil},
			},
			expectedProcessed: map[string]struct{}{
				"test error:*sentryzap.customError": {},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// Mock configuration
			cfg := &Configuration{DisableStacktrace: tc.disableStacktrace}
			c := &core{cfg: cfg}

			// Call the method
			result := c.addExceptionsFromError(tc.initialExceptions, tc.initialProcessed, tc.err)

			// Assert exceptions
			assert.Equal(t, len(tc.expectedExceptions), len(result), "Unexpected number of exceptions")
			for i, ex := range tc.expectedExceptions {
				assert.Equal(t, ex.Value, result[i].Value, "Mismatch in exception Value")
				assert.Equal(t, ex.Type, result[i].Type, "Mismatch in exception Type")
				if !tc.disableStacktrace {
					assert.NotNil(t, result[i].Stacktrace, "Expected Stacktrace to be set")
				} else {
					assert.Nil(t, result[i].Stacktrace, "Expected Stacktrace to be nil")
				}
			}

			// Assert processedErrors
			assert.Equal(t, tc.expectedProcessed, tc.initialProcessed, "Mismatch in processed errors map")
		})
	}
}
