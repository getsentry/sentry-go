package sentry

import (
	"errors"
	"fmt"
	"testing"

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
					Stacktrace: nil,
				},
			},
		},
		{
			name: "errors.Join with multiple errors",
			err:  errors.Join(errors.New("error A"), errors.New("error B")),
			expected: []Exception{
				{
					Value: "error B",
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
					Value: "error A\nerror B",
					Type:  "*errors.joinError",
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
					Value: "error B",
					Type:  "*errors.errorString",
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
						Source:           "cause",
						ExceptionID:      1,
						ParentID:         Pointer(0),
						IsExceptionGroup: true,
					},
				},
				{
					Value:      "wrapper: error A\nerror B",
					Type:       "*fmt.wrapError",
					Stacktrace: nil,
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertErrorToExceptions(tt.err)

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
// This simulates JavaScript's AggregateError for testing purposes
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
					Value: "rate limit exceeded",
					Type:  "*errors.errorString",
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
					Value: "Request failed due to multiple errors",
					Type:  "*sentry.AggregateError",
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
					Value: "field 'password' is too short",
					Type:  "*errors.errorString",
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
						Source:           "cause",
						ExceptionID:      1,
						ParentID:         Pointer(0),
						IsExceptionGroup: true,
					},
				},
				{
					Value: "operation failed: Multiple validation errors",
					Type:  "*fmt.wrapError",
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
