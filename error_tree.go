package sentry

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
)

type ErrorTree struct {
	Value    error
	Children []*ErrorTree
}

func createErrorTree(value error) *ErrorTree {
	if value == nil {
		return nil
	}

	root := &ErrorTree{
		Value:    value,
		Children: make([]*ErrorTree, 0),
	}

	errorTreeHelper(root)
	return root
}

func errorTreeHelper(node *ErrorTree) {
	if node.Value == nil {
		return
	}

	switch v := node.Value.(type) {
	case interface{ Unwrap() []error }:
		unwrapped := v.Unwrap()
		for _, err := range unwrapped {
			if err != nil {
				child := &ErrorTree{
					Value:    err,
					Children: make([]*ErrorTree, 0),
				}
				node.Children = append(node.Children, child)
				errorTreeHelper(child)
			}
		}
	case interface{ Unwrap() error }:
		unwrapped := v.Unwrap()
		if unwrapped != nil {
			child := &ErrorTree{
				Value:    unwrapped,
				Children: make([]*ErrorTree, 0),
			}
			node.Children = append(node.Children, child)
			errorTreeHelper(child)
		}
	case interface{ Cause() error }:
		cause := v.Cause()
		if cause != nil && !errors.Is(cause, node.Value) { // Avoid infinite recursion
			child := &ErrorTree{
				Value:    cause,
				Children: make([]*ErrorTree, 0),
			}
			node.Children = append(node.Children, child)
			errorTreeHelper(child)
		}
	}
}

// limitTreeDepth prunes the tree till the errors on the tree depth don't surpass maxErrorDepth
//
// The SDK uses the globally configured maxErrorDepth to specify the amount of errors that can be
// unwrapped without reaching the limit. This is different from the tree depth. The approach we follow
// to achieve consistency is to count the unwrapped errors on every tree depth level and if we surpassed
// the limit we drop the level and all lower ones.
func limitTreeDepth(tree *ErrorTree, maxErrorDepth int) {
	if tree == nil || maxErrorDepth < 0 {
		return
	}

	if maxErrorDepth == 0 {
		clearChildren(tree)
		return
	}

	errCount := 1 // include root
	queue := []*ErrorTree{tree}

	for len(queue) > 0 {
		var nextLevel []*ErrorTree

		for _, node := range queue {
			nextLevel = append(nextLevel, node.Children...)
		}

		if errCount+len(nextLevel) > maxErrorDepth {
			for _, node := range queue {
				clearChildren(node)
			}
			break
		}

		errCount += len(nextLevel)
		queue = nextLevel
	}
}

func clearChildren(node *ErrorTree) {
	if node == nil {
		return
	}
	for _, child := range node.Children {
		clearChildren(child)
	}
	node.Children = nil
}

func convertTreeToExceptions(tree *ErrorTree) []Exception {
	if tree == nil {
		return nil
	}

	var exceptions []Exception
	var idCounter int

	convertNodeDFS(tree, &exceptions, &idCounter, nil, "")

	slices.Reverse(exceptions)
	for i := range exceptions {
		exceptions[i].Mechanism.ExceptionID = len(exceptions) - 1 - i
	}

	return exceptions
}

func convertNodeDFS(node *ErrorTree, exceptions *[]Exception, idCounter *int, parentID *int, source string) {
	if node == nil || node.Value == nil {
		return
	}

	currentID := *idCounter
	*idCounter++

	exception := Exception{
		Value:      node.Value.Error(),
		Type:       reflect.TypeOf(node.Value).String(),
		Stacktrace: ExtractStacktrace(node.Value),
		Mechanism: &Mechanism{
			Type:        "generic",
			ExceptionID: currentID,
			ParentID:    parentID,
			Source:      source,
		},
	}

	if _, ok := node.Value.(interface{ Unwrap() []error }); ok {
		exception.Mechanism.IsExceptionGroup = true
	}

	*exceptions = append(*exceptions, exception)

	for i, child := range node.Children {
		var childSource string

		switch node.Value.(type) {
		case interface{ Unwrap() []error }:
			// Multiple errors, like from errors.Join
			childSource = fmt.Sprintf("errors[%d]", i)
		case interface{ Unwrap() error }:
			// Single wrapped error, like from fmt.Errorf
			childSource = "cause"
		case interface{ Cause() error }:
			// pkg/errors style
			childSource = "cause"
		default:
			childSource = "unknown"
		}

		convertNodeDFS(child, exceptions, idCounter, &currentID, childSource)
	}
}
