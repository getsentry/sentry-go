package sentry

import (
	"errors"
	"reflect"
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

	buildErrorTree(root)
	return root
}

func buildErrorTree(node *ErrorTree) {
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
				buildErrorTree(child)
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
			buildErrorTree(child)
		}
	case interface{ Cause() error }: // Handle pkg/errors.Cause
		cause := v.Cause()
		if cause != nil && !errors.Is(cause, node.Value) { // Avoid infinite recursion
			child := &ErrorTree{
				Value:    cause,
				Children: make([]*ErrorTree, 0),
			}
			node.Children = append(node.Children, child)
			buildErrorTree(child)
		}
	}
}

// flattenErrorTree returns a slice of all errors in the tree in depth-first order
func flattenErrorTree(tree *ErrorTree) []error {
	if tree == nil {
		return nil
	}

	var errors []error
	errors = append(errors, tree.Value)

	for _, child := range tree.Children {
		errors = append(errors, flattenErrorTree(child)...)
	}

	return errors
}

// convertTreeToExceptions converts an error tree to a slice of exceptions
// with proper parent-child relationships based on the RFC
func convertTreeToExceptions(tree *ErrorTree) []Exception {
	if tree == nil {
		return nil
	}

	var exceptions []Exception
	var idCounter int

	// Convert tree to exceptions using depth-first traversal
	convertNode(tree, &exceptions, &idCounter, nil)

	return exceptions
}

func convertNode(node *ErrorTree, exceptions *[]Exception, idCounter *int, parentID *int) {
	if node == nil || node.Value == nil {
		return
	}

	currentID := *idCounter
	*idCounter++

	// Create exception for current node
	exception := Exception{
		Value:      node.Value.Error(),
		Type:       reflect.TypeOf(node.Value).String(),
		Stacktrace: ExtractStacktrace(node.Value),
		Mechanism: &Mechanism{
			Type:        "generic",
			ExceptionID: currentID,
		},
	}

	// Set parent relationship if this is not the root
	if parentID != nil {
		exception.Mechanism.ParentID = parentID
	}

	// Mark as exception group if it has children
	if len(node.Children) > 0 {
		exception.Mechanism.IsExceptionGroup = true
	}

	*exceptions = append(*exceptions, exception)

	// Process children
	for _, child := range node.Children {
		convertNode(child, exceptions, idCounter, &currentID)
	}
}
