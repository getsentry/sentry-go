package sentry

import (
	"errors"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestErrorTree(t *testing.T) {
	testCases := map[string]struct {
		root     error
		expected []Exception
	}{
		"Single error without unwrap": {
			root: errors.New("simple error"),
			expected: []Exception{
				{
					Value:     "simple error",
					Type:      "*errors.errorString",
					Mechanism: &Mechanism{Type: "generic", IsExceptionGroup: true},
				},
			},
		},
		"Nested errors with Unwrap": {
			root: fmt.Errorf("level 2: %w", fmt.Errorf("level 1: %w", errors.New("base error"))),
			expected: []Exception{
				{
					Value: "base error",
					Type:  "*errors.errorString",
					Mechanism: &Mechanism{
						Type:             "generic",
						ExceptionID:      0,
						IsExceptionGroup: true,
					},
				},
				{
					Value: "level 1: base error",
					Type:  "*fmt.wrapError",
					Mechanism: &Mechanism{
						Type:             "generic",
						ExceptionID:      1,
						ParentID:         Pointer(0),
						IsExceptionGroup: true,
					},
				},
				{
					Value: "level 2: level 1: base error",
					Type:  "*fmt.wrapError",
					Mechanism: &Mechanism{
						Type:             "generic",
						ExceptionID:      2,
						ParentID:         Pointer(1),
						IsExceptionGroup: true,
					},
				},
			},
		},
		// TODO: more cases from TestSetException.
		"Simple two-error join": {
			root: errors.Join(
				errors.New("0"),
				errors.New("1"),
			),
			expected: []Exception{
				{
					Value:      "0",
					Type:       "*errors.errorString",
					Stacktrace: nil,
					Mechanism:  &Mechanism{Type: "generic", IsExceptionGroup: true},
				},
				{
					Value:      "1",
					Type:       "*errors.errorString",
					Stacktrace: nil,
					Mechanism:  &Mechanism{Type: "generic", IsExceptionGroup: true, ExceptionID: 1},
				},
				{
					Value:      "0\n1",
					Type:       "*errors.joinError",
					Stacktrace: nil,
					Mechanism:  &Mechanism{Type: "generic", IsExceptionGroup: true, ExceptionID: 2},
				},
			},
		},
		"Complex join": {
			root: errors.Join(
				errors.Join(
					errors.New("0"),
					errors.New("1"),
				),
				errors.New("2"),
			),
			expected: []Exception{
				{
					Value:      "0",
					Type:       "*errors.errorString",
					Stacktrace: nil,
					Mechanism:  &Mechanism{Type: "generic", IsExceptionGroup: true},
				},
				{
					Value:      "1",
					Type:       "*errors.errorString",
					Stacktrace: nil,
					Mechanism:  &Mechanism{Type: "generic", IsExceptionGroup: true, ExceptionID: 1},
				},
				{
					Value:      "0\n1",
					Type:       "*errors.joinError",
					Stacktrace: nil,
					Mechanism:  &Mechanism{Type: "generic", IsExceptionGroup: true, ExceptionID: 2},
				},
				{
					Value:      "2",
					Type:       "*errors.errorString",
					Stacktrace: nil,
					Mechanism:  &Mechanism{Type: "generic", IsExceptionGroup: true, ExceptionID: 3},
				},
				{
					Value:      "0\n1\n2",
					Type:       "*errors.joinError",
					Stacktrace: nil,
					Mechanism:  &Mechanism{Type: "generic", IsExceptionGroup: true, ExceptionID: 4},
				},
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			tree := errorTree{tc.root}
			result := tree.Exceptions()

			if len(result) != len(tc.expected) {
				t.Fatalf("Expected %d exceptions, got %d", len(tc.expected), len(result))
			}

			for i, exp := range tc.expected {
				if diff := cmp.Diff(exp, result[i]); diff != "" {
					t.Errorf("Event mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestErrorTree_JoinComplex(t *testing.T) {
	err := errors.Join(
		errors.Join(
			errors.New("0"),
			errors.New("1"),
		),
		errors.New("2"),
	)
	tree := errorTree{err}

	exceptions := tree.Exceptions()

	expected := []Exception{
		{
			Value:      "0",
			Type:       "*errors.errorString",
			Stacktrace: nil,
			Mechanism:  &Mechanism{Type: "generic", IsExceptionGroup: true},
		},
		{
			Value:      "1",
			Type:       "*errors.errorString",
			Stacktrace: nil,
			Mechanism:  &Mechanism{Type: "generic", IsExceptionGroup: true, ExceptionID: 1},
		},
		{
			Value:      "0\n1",
			Type:       "*errors.joinError",
			Stacktrace: nil,
			Mechanism:  &Mechanism{Type: "generic", IsExceptionGroup: true, ExceptionID: 2},
		},
		{
			Value:      "2",
			Type:       "*errors.errorString",
			Stacktrace: nil,
			Mechanism:  &Mechanism{Type: "generic", IsExceptionGroup: true, ExceptionID: 3},
		},
		{
			Value:      "0\n1\n2",
			Type:       "*errors.joinError",
			Stacktrace: nil,
			Mechanism:  &Mechanism{Type: "generic", IsExceptionGroup: true, ExceptionID: 4},
		},
	}

	if len(exceptions) != len(expected) {
		t.Fatalf("Expected %d exceptions, got %d", len(expected), len(exceptions))
	}

	for i, exp := range expected {
		if diff := cmp.Diff(exp, exceptions[i]); diff != "" {
			t.Errorf("Event mismatch (-want +got):\n%s", diff)
		}
	}
}
