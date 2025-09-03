package sentry

import (
	"errors"
	"fmt"
	"testing"
)

type errorWithCause struct {
	msg   string
	cause error
}

func (e *errorWithCause) Error() string { return e.msg }
func (e *errorWithCause) Cause() error  { return e.cause }

type circularError struct {
	msg  string
	self error
}

func (e *circularError) Error() string { return e.msg }
func (e *circularError) Cause() error  { return e.self }

func TestCreateErrorTree(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected *ErrorTree
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: nil,
		},
		{
			name: "simple error",
			err:  errors.New("simple error"),
			expected: &ErrorTree{
				Value:    errors.New("simple error"),
				Children: []*ErrorTree{},
			},
		},
		{
			name: "fmt.Errorf wrapped error",
			err:  fmt.Errorf("wrapped: %w", errors.New("base error")),
			expected: &ErrorTree{
				Value: fmt.Errorf("wrapped: %w", errors.New("base error")),
				Children: []*ErrorTree{
					{
						Value:    errors.New("base error"),
						Children: []*ErrorTree{},
					},
				},
			},
		},
		{
			name: "errors.Join multiple errors",
			err:  errors.Join(errors.New("error1"), errors.New("error2"), errors.New("error3")),
			expected: &ErrorTree{
				Value: errors.Join(errors.New("error1"), errors.New("error2"), errors.New("error3")),
				Children: []*ErrorTree{
					{
						Value:    errors.New("error1"),
						Children: []*ErrorTree{},
					},
					{
						Value:    errors.New("error2"),
						Children: []*ErrorTree{},
					},
					{
						Value:    errors.New("error3"),
						Children: []*ErrorTree{},
					},
				},
			},
		},
		{
			name: "pkg/errors style Cause",
			err: &errorWithCause{
				msg:   "wrapper error",
				cause: errors.New("root cause"),
			},
			expected: &ErrorTree{
				Value: &errorWithCause{
					msg:   "wrapper error",
					cause: errors.New("root cause"),
				},
				Children: []*ErrorTree{
					{
						Value:    errors.New("root cause"),
						Children: []*ErrorTree{},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := createErrorTree(tt.err)

			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil, got %+v", result)
				}
				return
			}

			if result == nil {
				t.Fatal("expected non-nil result")
			}

			if !compareErrorTrees(result, tt.expected) {
				t.Errorf("error trees don't match.\nExpected: %+v\nGot: %+v", tt.expected, result)
			}
		})
	}
}

func TestCreateErrorTreeComplex(t *testing.T) {
	baseErr1 := errors.New("database error")
	baseErr2 := errors.New("network error")
	joinedErr := errors.Join(baseErr1, baseErr2)
	wrappedErr := fmt.Errorf("request failed: %w", joinedErr)

	tree := createErrorTree(wrappedErr)

	if tree == nil {
		t.Fatal("expected non-nil tree")
	}

	if len(tree.Children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(tree.Children))
	}

	joinedNode := tree.Children[0]
	if len(joinedNode.Children) != 2 {
		t.Fatalf("expected 2 children for joined error, got %d", len(joinedNode.Children))
	}

	child1 := joinedNode.Children[0]
	child2 := joinedNode.Children[1]

	if child1.Value.Error() != "database error" {
		t.Errorf("expected 'database error', got '%s'", child1.Value.Error())
	}
	if child2.Value.Error() != "network error" {
		t.Errorf("expected 'network error', got '%s'", child2.Value.Error())
	}
}

func TestCreateErrorTreeCircularReference(t *testing.T) {
	circErr := &circularError{msg: "circular error"}
	circErr.self = circErr

	tree := createErrorTree(circErr)

	if tree == nil {
		t.Fatal("expected non-nil tree")
	}

	if len(tree.Children) != 0 {
		t.Errorf("expected no children due to circular reference, got %d children", len(tree.Children))
	}
}

func TestLimitTreeDepth(t *testing.T) {
	level2 := errors.New("level2")
	level1 := errors.Join(fmt.Errorf("level1: %w", level2), errors.New("err1"))
	root := errors.Join(level1, errors.New("level0"))

	// Tree structure:
	// root (errors.Join) -> 2 children at depth 1: level1, level0
	// level1 (errors.Join) -> 2 children at depth 2: "level1: level2", err1
	// "level1: level2" (fmt.Errorf) -> 1 child at depth 3: level2
	// Total errors: 6, Tree depth: 3

	tests := []struct {
		name              string
		maxDepth          int
		expectedTreeDepth int
		expectedChildren  int
	}{
		{
			name:              "maxErrorDepth 0 - clears all children",
			maxDepth:          0,
			expectedTreeDepth: 0,
			expectedChildren:  0,
		},
		{
			name:              "maxErrorDepth 1 - only root, no children (1 error total)",
			maxDepth:          1,
			expectedTreeDepth: 0,
			expectedChildren:  0,
		},
		{
			name:              "maxErrorDepth 2 - root only (level 1 would exceed limit: 1+2=3 > 2)",
			maxDepth:          2,
			expectedTreeDepth: 0,
			expectedChildren:  0,
		},
		{
			name:              "maxErrorDepth 3 - root + 2 errors from depth 1 (3 errors total)",
			maxDepth:          3,
			expectedTreeDepth: 1,
			expectedChildren:  2,
		},
		{
			name:              "maxErrorDepth 4 - root + depth 1 only (level 2 would exceed limit: 1+2+2=5 > 4)",
			maxDepth:          4,
			expectedTreeDepth: 1,
			expectedChildren:  2,
		},
		{
			name:              "maxErrorDepth 5 - root + depth 1 + 2 errors from depth 2",
			maxDepth:          5,
			expectedTreeDepth: 2,
			expectedChildren:  2,
		},
		{
			name:              "maxErrorDepth 6 - all errors included",
			maxDepth:          6,
			expectedTreeDepth: 3,
			expectedChildren:  2,
		},
		{
			name:              "maxErrorDepth 9 - all errors included (more than needed)",
			maxDepth:          9,
			expectedTreeDepth: 3,
			expectedChildren:  2,
		},
		{
			name:              "negative depth - returns early, no change to tree",
			maxDepth:          -1,
			expectedTreeDepth: 3,
			expectedChildren:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tree := createErrorTree(root)
			limitTreeDepth(tree, tt.maxDepth)

			depth := getTreeDepth(tree)
			if depth != tt.expectedTreeDepth {
				t.Errorf("expected depth %d, got %d", tt.expectedTreeDepth, depth)
			}

			if len(tree.Children) != tt.expectedChildren {
				t.Errorf("expected %d children at root, got %d", tt.expectedChildren, len(tree.Children))
			}
		})
	}
}

func TestLimitTreeDepthNilTree(t *testing.T) {
	limitTreeDepth(nil, 5)
}

func TestLimitTreeDepthWithJoinedErrors(t *testing.T) {
	err1 := errors.New("error1")
	err2 := errors.New("error2")
	err3 := errors.New("error3")
	joined := errors.Join(err1, err2, err3)

	tree := createErrorTree(joined)

	if len(tree.Children) != 3 {
		t.Fatalf("expected 3 children before limiting, got %d", len(tree.Children))
	}

	limitTreeDepth(tree, 0)
	if len(tree.Children) != 0 {
		t.Errorf("expected 0 children after depth limit 0, got %d", len(tree.Children))
	}
}

func TestConvertTreeToExceptions(t *testing.T) {
	tests := []struct {
		name     string
		tree     *ErrorTree
		expected []Exception
	}{
		{
			name:     "nil tree",
			tree:     nil,
			expected: nil,
		},
		{
			name: "single error tree",
			tree: &ErrorTree{
				Value:    errors.New("single error"),
				Children: []*ErrorTree{},
			},
			expected: []Exception{
				{
					Value:      "single error",
					Type:       "*errors.errorString",
					Stacktrace: nil,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertTreeToExceptions(tt.tree)

			if tt.expected == nil {
				if result != nil {
					t.Errorf("expected nil result, got %+v", result)
				}
				return
			}

			if len(result) != len(tt.expected) {
				t.Fatalf("expected %d exceptions, got %d", len(tt.expected), len(result))
			}

			for i, expected := range tt.expected {
				actual := result[i]
				if actual.Value != expected.Value {
					t.Errorf("exception %d: expected value %q, got %q", i, expected.Value, actual.Value)
				}
				if actual.Type != expected.Type {
					t.Errorf("exception %d: expected type %q, got %q", i, expected.Type, actual.Type)
				}
			}
		})
	}
}

func compareErrorTrees(a, b *ErrorTree) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	if a.Value.Error() != b.Value.Error() {
		return false
	}

	if len(a.Children) != len(b.Children) {
		return false
	}

	for i := 0; i < len(a.Children); i++ {
		if !compareErrorTrees(a.Children[i], b.Children[i]) {
			return false
		}
	}

	return true
}

func getTreeDepth(tree *ErrorTree) int {
	if tree == nil || len(tree.Children) == 0 {
		return 0
	}

	maxChildDepth := 0
	for _, child := range tree.Children {
		childDepth := getTreeDepth(child)
		if childDepth > maxChildDepth {
			maxChildDepth = childDepth
		}
	}

	return maxChildDepth + 1
}
