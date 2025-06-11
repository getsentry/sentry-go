package sentry

import (
	"errors"
	"reflect"
)

type errorTree struct {
	Err error
}

func (et errorTree) children() []errorTree {
	if et.Err == nil {
		return nil
	}

	// Attempt to unwrap the error using the standard library's Unwrap method.
	if unwrappedErr := errors.Unwrap(et.Err); unwrappedErr != nil {
		return []errorTree{{unwrappedErr}}
	}
	// The error implements the Cause method, indicating it may have been wrapped
	// using the github.com/pkg/errors package.
	if caused, ok := et.Err.(interface{ Cause() error }); ok && caused.Cause() != nil {
		return []errorTree{{caused.Cause()}}
	}
	// A non-nil error returned by `errors.Join` implements `Unwrap() []error`.
	if joined, ok := et.Err.(interface{ Unwrap() []error }); ok {
		members := joined.Unwrap()
		children := make([]errorTree, len(members))
		for i := range members {
			children[i] = errorTree{members[i]}
		}
		return children
	}
	return []errorTree{}
}

// Exceptions in et, ordered from earliest to latest. All exception Mechanisms
// returned here are as if the exceptions belong to a group.
func (et errorTree) Exceptions() []Exception {
	if et.Err == nil {
		return nil
	}

	// Child exceptions are chronologically earlier.
	children := et.children()
	exceptions := make([]Exception, 0, len(children))
	for _, child := range children {
		exceptions = join(exceptions, child.Exceptions()...)
	}

	// Append this error.
	mechanism := &Mechanism{
		IsExceptionGroup: true,
		ExceptionID:      len(exceptions),
		Type:             "generic",
	}
	if len(children) == 1 {
		// Part of a causal chain; record parent-child relationship.
		lastChild := exceptions[len(exceptions)-1]
		mechanism.ParentID = Pointer(lastChild.Mechanism.ExceptionID)
	}
	exceptions = append(exceptions, Exception{
		Value:      et.Err.Error(),
		Type:       reflect.TypeOf(et.Err).String(),
		Stacktrace: ExtractStacktrace(et.Err),
		Mechanism:  mechanism,
	})
	return exceptions
}

// join Exception sequences, adjusting indexed IDs (ExceptionID, ParentID).
func join(prior []Exception, latter ...Exception) []Exception {
	for i := range latter {
		latter[i].Mechanism.ExceptionID += len(prior)
		if latter[i].Mechanism.ParentID != nil {
			parentID := latter[i].Mechanism.ParentID
			latter[i].Mechanism.ParentID = Pointer(len(prior) + *parentID)
		}
	}
	return append(prior, latter...)
}
