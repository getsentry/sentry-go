package sentry

import (
	"errors"
	"fmt"
	"reflect"
)

const (
	MechanismTypeGeneric string = "generic"
	MechanismTypeChained string = "chained"

	MechanismSourceCause string = "cause"
)

type ErrorTree struct {
	Value         error
	Children      []*ErrorTree
	Source        string
	MechanismType string
}

func createErrorTree(value error) *ErrorTree {
	if value == nil {
		return nil
	}

	root := &ErrorTree{
		Value:         value,
		Children:      make([]*ErrorTree, 0),
		MechanismType: MechanismTypeGeneric,
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
		for i, err := range unwrapped {
			if err != nil {
				child := &ErrorTree{
					Value:         err,
					Children:      make([]*ErrorTree, 0),
					Source:        fmt.Sprintf("errors[%d]", i),
					MechanismType: MechanismTypeChained,
				}
				node.Children = append(node.Children, child)
				errorTreeHelper(child)
			}
		}
	case interface{ Unwrap() error }:
		unwrapped := v.Unwrap()
		if unwrapped != nil {
			child := &ErrorTree{
				Value:         unwrapped,
				Children:      make([]*ErrorTree, 0),
				Source:        MechanismSourceCause,
				MechanismType: MechanismTypeChained,
			}
			node.Children = append(node.Children, child)
			errorTreeHelper(child)
		}
	case interface{ Cause() error }:
		cause := v.Cause()
		if cause != nil && !errors.Is(cause, node.Value) { // Avoid infinite recursion
			child := &ErrorTree{
				Value:         cause,
				Children:      make([]*ErrorTree, 0),
				Source:        MechanismSourceCause,
				MechanismType: MechanismTypeChained,
			}
			node.Children = append(node.Children, child)
			errorTreeHelper(child)
		}
	default:
		node.MechanismType = ""
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

	convertNodePostOrder(tree, &exceptions)

	if len(exceptions) > 1 {
		for i := range exceptions {
			if exceptions[i].Mechanism != nil {
				if i == 0 {
					exceptions[i].Mechanism.Source = ""
				} else {
					parentID := i - 1
					exceptions[i].Mechanism.ParentID = &parentID
				}
			}
		}
	} else if len(exceptions) == 1 {
		exceptions[0].Mechanism = nil
	}

	return exceptions
}

func convertNodePostOrder(node *ErrorTree, exceptions *[]Exception) {
	if node == nil || node.Value == nil {
		return
	}

	// to adhere with the post order search we need to reverse all the children originating from errors.Join.
	if _, isJoin := node.Value.(interface{ Unwrap() []error }); isJoin {
		for i := len(node.Children) - 1; i >= 0; i-- {
			convertNodePostOrder(node.Children[i], exceptions)
		}
	} else {
		for _, child := range node.Children {
			convertNodePostOrder(child, exceptions)
		}
	}

	exception := Exception{
		Value:      node.Value.Error(),
		Type:       reflect.TypeOf(node.Value).String(),
		Stacktrace: ExtractStacktrace(node.Value),
	}

	exception.Mechanism = &Mechanism{
		Type:             node.MechanismType,
		ExceptionID:      len(*exceptions),
		ParentID:         nil, // Will be set in post-processing
		Source:           node.Source,
		IsExceptionGroup: true,
	}

	*exceptions = append(*exceptions, exception)
}
