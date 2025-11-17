package sentry

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestConvertErrorToExceptions(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected []Exception
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: nil,
		},
		{
			name: "single error",
			err:  errors.New("single error"),
			expected: []Exception{
				{
					Value:      "single error",
					Type:       "*errors.errorString",
					Stacktrace: &Stacktrace{Frames: []Frame{}},
				},
			},
		},
		{
			name: "errors.Join with multiple errors",
			err:  errors.Join(errors.New("error A"), errors.New("error B")),
			expected: []Exception{
				{
					Value:      "error B",
					Type:       "*errors.errorString",
					Stacktrace: nil,
					Mechanism: &Mechanism{
						Type:             "chained",
						Source:           "errors[1]",
						ExceptionID:      2,
						ParentID:         Pointer(0),
						IsExceptionGroup: false,
					},
				},
				{
					Value: "error A",
					Type:  "*errors.errorString",
					Mechanism: &Mechanism{
						Type:             "chained",
						Source:           "errors[0]",
						ExceptionID:      1,
						ParentID:         Pointer(0),
						IsExceptionGroup: false,
					},
				},
				{
					Value:      "error A\nerror B",
					Type:       "*errors.joinError",
					Stacktrace: &Stacktrace{Frames: []Frame{}},
					Mechanism: &Mechanism{
						Type:             "generic",
						Source:           "",
						ExceptionID:      0,
						ParentID:         nil,
						IsExceptionGroup: true,
					},
				},
			},
		},
		{
			name: "nested wrapped error with errors.Join",
			err:  fmt.Errorf("wrapper: %w", errors.Join(errors.New("error A"), errors.New("error B"))),
			expected: []Exception{
				{
					Value:      "error B",
					Type:       "*errors.errorString",
					Stacktrace: nil,
					Mechanism: &Mechanism{
						Type:             "chained",
						Source:           "errors[1]",
						ExceptionID:      3,
						ParentID:         Pointer(1),
						IsExceptionGroup: false,
					},
				},
				{
					Value: "error A",
					Type:  "*errors.errorString",
					Mechanism: &Mechanism{
						Type:             "chained",
						Source:           "errors[0]",
						ExceptionID:      2,
						ParentID:         Pointer(1),
						IsExceptionGroup: false,
					},
				},
				{
					Value: "error A\nerror B",
					Type:  "*errors.joinError",
					Mechanism: &Mechanism{
						Type:             "chained",
						Source:           MechanismTypeUnwrap,
						ExceptionID:      1,
						ParentID:         Pointer(0),
						IsExceptionGroup: true,
					},
				},
				{
					Value:      "wrapper: error A\nerror B",
					Type:       "*fmt.wrapError",
					Stacktrace: &Stacktrace{Frames: []Frame{}},
					Mechanism: &Mechanism{
						Type:             "generic",
						Source:           "",
						ExceptionID:      0,
						ParentID:         nil,
						IsExceptionGroup: false,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertErrorToExceptions(tt.err, -1)

			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil result, got %+v", result)
				}
				return
			}

			if diff := cmp.Diff(tt.expected, result); diff != "" {
				t.Errorf("Exception mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// AggregateError represents multiple errors occurring together
// This simulates JavaScript's AggregateError for testing purposes.
type AggregateError struct {
	Message string
	Errors  []error
}

func (e *AggregateError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return "Multiple errors occurred"
}

func (e *AggregateError) Unwrap() []error {
	return e.Errors
}

func TestExceptionGroupsWithAggregateError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected []Exception
	}{
		{
			name: "AggregateError with custom message",
			err: &AggregateError{
				Message: "Request failed due to multiple errors",
				Errors: []error{
					errors.New("network timeout"),
					errors.New("authentication failed"),
					errors.New("rate limit exceeded"),
				},
			},
			expected: []Exception{
				{
					Value:      "rate limit exceeded",
					Type:       "*errors.errorString",
					Stacktrace: nil,
					Mechanism: &Mechanism{
						Type:             "chained",
						Source:           "errors[2]",
						ExceptionID:      3,
						ParentID:         Pointer(0),
						IsExceptionGroup: false,
					},
				},
				{
					Value: "authentication failed",
					Type:  "*errors.errorString",
					Mechanism: &Mechanism{
						Type:             "chained",
						Source:           "errors[1]",
						ExceptionID:      2,
						ParentID:         Pointer(0),
						IsExceptionGroup: false,
					},
				},
				{
					Value: "network timeout",
					Type:  "*errors.errorString",
					Mechanism: &Mechanism{
						Type:             "chained",
						Source:           "errors[0]",
						ExceptionID:      1,
						ParentID:         Pointer(0),
						IsExceptionGroup: false,
					},
				},
				{
					Value:      "Request failed due to multiple errors",
					Type:       "*sentry.AggregateError",
					Stacktrace: &Stacktrace{Frames: []Frame{}},
					Mechanism: &Mechanism{
						Type:             "generic",
						Source:           "",
						ExceptionID:      0,
						ParentID:         nil,
						IsExceptionGroup: true,
					},
				},
			},
		},
		{
			name: "Nested AggregateError with wrapper",
			err: fmt.Errorf("operation failed: %w", &AggregateError{
				Message: "Multiple validation errors",
				Errors: []error{
					errors.New("field 'email' is required"),
					errors.New("field 'password' is too short"),
				},
			}),
			expected: []Exception{
				{
					Value:      "field 'password' is too short",
					Type:       "*errors.errorString",
					Stacktrace: nil,
					Mechanism: &Mechanism{
						Type:             "chained",
						Source:           "errors[1]",
						ExceptionID:      3,
						ParentID:         Pointer(1),
						IsExceptionGroup: false,
					},
				},
				{
					Value: "field 'email' is required",
					Type:  "*errors.errorString",
					Mechanism: &Mechanism{
						Type:             "chained",
						Source:           "errors[0]",
						ExceptionID:      2,
						ParentID:         Pointer(1),
						IsExceptionGroup: false,
					},
				},
				{
					Value: "Multiple validation errors",
					Type:  "*sentry.AggregateError",
					Mechanism: &Mechanism{
						Type:             "chained",
						Source:           MechanismTypeUnwrap,
						ExceptionID:      1,
						ParentID:         Pointer(0),
						IsExceptionGroup: true,
					},
				},
				{
					Value:      "operation failed: Multiple validation errors",
					Type:       "*fmt.wrapError",
					Stacktrace: &Stacktrace{Frames: []Frame{}},
					Mechanism: &Mechanism{
						Type:             "generic",
						Source:           "",
						ExceptionID:      0,
						ParentID:         nil,
						IsExceptionGroup: false,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &Event{}
			event.SetException(tt.err, 10) // Use high max depth

			if diff := cmp.Diff(tt.expected, event.Exception); diff != "" {
				t.Errorf("Exception mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

type CircularError struct {
	Message string
	Next    error
}

func (e *CircularError) Error() string {
	return e.Message
}

func (e *CircularError) Unwrap() error {
	return e.Next
}

func TestCircularReferenceProtection(t *testing.T) {
	tests := []struct {
		name        string
		setupError  func() error
		description string
		maxDepth    int
	}{
		{
			name: "self-reference",
			setupError: func() error {
				err := &CircularError{Message: "self-referencing error"}
				err.Next = err
				return err
			},
			description: "Error that directly references itself",
			maxDepth:    1,
		},
		{
			name: "chain-loop",
			setupError: func() error {
				err1 := &CircularError{Message: "error A"}
				err2 := &CircularError{Message: "error B"}
				err1.Next = err2
				err2.Next = err1 // Creates A -> B -> A cycle
				return err1
			},
			description: "Two errors that reference each other in a cycle",
			maxDepth:    2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.setupError()

			start := time.Now()
			exceptions := convertErrorToExceptions(err, -1)
			duration := time.Since(start)

			if duration > 100*time.Millisecond {
				t.Errorf("convertErrorToExceptions took too long: %v, possible infinite recursion", duration)
			}

			if len(exceptions) == 0 {
				t.Error("Expected at least one exception, got none")
				return
			}

			if len(exceptions) != tt.maxDepth {
				t.Errorf("Expected exactly %d exceptions (before cycle detection), got %d", tt.maxDepth, len(exceptions))
			}

			for i, exception := range exceptions {
				if exception.Value == "" {
					t.Errorf("Exception %d has empty value", i)
				}
				if exception.Type == "" {
					t.Errorf("Exception %d has empty type", i)
				}
			}

			t.Logf("✓ Successfully handled %s: got %d exceptions in %v", tt.description, len(exceptions), duration)
		})
	}
}

// unhashableSliceError is a non-comparable error type.
type unhashableSliceError []string

func (e unhashableSliceError) Error() string {
	return "unhashable slice error"
}

func TestConvertErrorToExceptions_UnhashableError_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("convertErrorToExceptions panicked for unhashable error: %v", r)
		}
	}()

	err := unhashableSliceError{"a", "b"}
	_ = convertErrorToExceptions(err, -1)
}

type wrapper struct {
	V any
}

func (w wrapper) Error() string {
	return ""
}

func TestConvertErrorToExceptions_UnhashableWrapper_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("convertErrorToExceptions panicked for unhashable error: %v", r)
		}
	}()

	var err error = wrapper{unhashableSliceError{"a", "b"}}
	_ = convertErrorToExceptions(err, -1)
}
