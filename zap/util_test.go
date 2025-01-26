package sentryzap

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap/zapcore"
)

type matcher struct {
	matches func(sentry.Frame) bool
}

func (m *matcher) Matches(s sentry.Frame) bool {
	return m.matches(s)
}

func TestSetDefaultConfig(t *testing.T) {
	cfg := &Configuration{}

	// Test setting default values
	setDefaultConfig(cfg)
	assert.Equal(t, defaultMaxBreadcrumbs, cfg.MaxBreadcrumbs, "Expected MaxBreadcrumbs to be set to default")
	assert.Equal(t, 3*time.Second, cfg.FlushTimeout, "Expected FlushTimeout to be set to default")
	assert.NotNil(t, cfg.FrameMatcher, "Expected FrameMatcher to be set to default")

	// Test preserving non-default values
	m := &matcher{}
	cfg = &Configuration{
		MaxBreadcrumbs: 50,
		FlushTimeout:   5 * time.Second,
		FrameMatcher:   m,
	}

	setDefaultConfig(cfg)

	assert.Equal(t, 50, cfg.MaxBreadcrumbs, "Expected MaxBreadcrumbs to remain unchanged")
	assert.Equal(t, 5*time.Second, cfg.FlushTimeout, "Expected FlushTimeout to remain unchanged")
	assert.Equal(t, m, cfg.FrameMatcher, "Expected FrameMatcher to remain unchanged")
	assert.Same(t, m, cfg.FrameMatcher, "Expected FrameMatcher to be the exact same instance")
}

func TestUnwrapError(t *testing.T) {
	// Test unwrapping an error with Unwrap method
	err := &customError{message: "original error"}
	unwrapped := unwrapError(err)
	assert.Nil(t, unwrapped, "Expected no unwrapped error for customError without Unwrap")

	causeErr := &nestedError{
		message: "nested error",
		cause:   &customError{message: "cause unwrapped error"},
	}

	unwrapped = unwrapError(causeErr)
	assert.EqualError(t, unwrapped, "cause unwrapped error", "Expected to unwrap to 'cause unwrapped error'")

	// Test with a regular error
	newErr := errors.New("regular error")
	unwrapped = unwrapError(newErr)
	assert.Nil(t, unwrapped, "Expected no unwrapped error for regular error")
}

type typeNameError struct{}

func (typeNameError) Error() string { return "type name error" }
func (typeNameError) TypeName() string {
	return "CustomTypeName"
}

func TestGetTypeName(t *testing.T) {
	// Test an error with TypeName method
	err := typeNameError{}
	typeName := getTypeName(err)
	assert.Equal(t, "CustomTypeName", typeName, "Expected TypeName to return 'CustomTypeName'")

	// Test a regular error
	newErr := errors.New("generic error")
	typeName = getTypeName(newErr)
	assert.Equal(t, "*errors.errorString", typeName, "Expected TypeName to return type string for generic error")
}

func TestGetTypeOf(t *testing.T) {
	err := errors.New("test error")
	typeOf := getTypeOf(err)
	expected := fmt.Sprintf("test error:%s", reflect.TypeOf(err).String())
	assert.Equal(t, expected, typeOf, "Expected getTypeOf to return 'error:reflect.Type'")
}

func TestGetScope(t *testing.T) {
	scope := sentry.NewScope()
	field := zapcore.Field{
		Key:       sentryZapScopeKey,
		Type:      zapcore.SkipType,
		Interface: scope,
	}

	// Test valid scope field
	result := getScope(field)
	assert.Equal(t, scope, result, "Expected getScope to return the provided scope")

	// Test field with incorrect key
	field.Key = "wrong_key"
	result = getScope(field)
	assert.Nil(t, result, "Expected getScope to return nil for incorrect key")

	// Test field with incorrect type
	field.Key = sentryZapScopeKey
	field.Type = zapcore.StringType
	result = getScope(field)
	assert.Nil(t, result, "Expected getScope to return nil for incorrect type")

	// Test field with nil interface
	field.Interface = nil
	result = getScope(field)
	assert.Nil(t, result, "Expected getScope to return nil for nil interface")
}

func TestFilterFrames(t *testing.T) {
	tests := map[string]struct {
		frames         []sentry.Frame
		matcherFunc    func(sentry.Frame) bool
		expectedFrames []sentry.Frame
	}{
		"No frames match": {
			frames: []sentry.Frame{
				{Function: "func1", Module: "module1"},
				{Function: "func2", Module: "module2"},
			},
			matcherFunc: func(frame sentry.Frame) bool {
				return false // No frames match the condition
			},
			expectedFrames: []sentry.Frame{
				{Function: "func1", Module: "module1"},
				{Function: "func2", Module: "module2"},
			},
		},
		"All frames match": {
			frames: []sentry.Frame{
				{Function: "func1", Module: "module1"},
				{Function: "func2", Module: "module2"},
			},
			matcherFunc: func(frame sentry.Frame) bool {
				return true // All frames match the condition
			},
			expectedFrames: []sentry.Frame{}, // All frames are filtered out
		},
		"Some frames match": {
			frames: []sentry.Frame{
				{Function: "func1", Module: "module1"},
				{Function: "func2", Module: "module2"},
				{Function: "func3", Module: "module3"},
			},
			matcherFunc: func(frame sentry.Frame) bool {
				return frame.Function == "func2" // Only "func2" matches the condition
			},
			expectedFrames: []sentry.Frame{
				{Function: "func1", Module: "module1"},
				{Function: "func3", Module: "module3"},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// Create a mock matcher
			mockMatcher := &matcher{
				matches: tc.matcherFunc,
			}

			// Create a core instance with the mock matcher
			cfg := &Configuration{FrameMatcher: mockMatcher}
			core := &core{cfg: cfg}

			// Call filterFrames
			filteredFrames := core.filterFrames(tc.frames)

			// Assert the result
			assert.Equal(t, tc.expectedFrames, filteredFrames, "Filtered frames do not match expected frames")
		})
	}
}
