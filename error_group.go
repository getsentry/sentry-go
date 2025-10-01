package sentry

import (
	"fmt"
	"reflect"
	"slices"
)

const (
	MechanismTypeGeneric string = "generic"
	MechanismTypeChained string = "chained"

	MechanismSourceCause string = "cause"
)

func convertErrorToExceptions(err error) []Exception {
	var exceptions []Exception
	visited := make(map[error]bool)
	convertErrorDFS(err, &exceptions, nil, "", visited)

	// mechanism type is used for debugging purposes, but since we can't really distinguish the origin of who invoked
	// captureException, we set it to nil if the error is not chained.
	if len(exceptions) == 1 {
		exceptions[0].Mechanism = nil
	}

	slices.Reverse(exceptions)

	return exceptions
}

func convertErrorDFS(err error, exceptions *[]Exception, parentID *int, source string, visited map[error]bool) {
	if err == nil {
		return
	}

	if visited[err] {
		return
	}
	visited[err] = true

	var isExceptionGroup bool

	switch err.(type) {
	case interface{ Unwrap() []error }, interface{ Unwrap() error }, interface{ Cause() error }:
		isExceptionGroup = true
	}

	exception := Exception{
		Value:      err.Error(),
		Type:       reflect.TypeOf(err).String(),
		Stacktrace: ExtractStacktrace(err),
	}

	currentID := len(*exceptions)

	var mechanismType string

	if parentID == nil {
		mechanismType = MechanismTypeGeneric
		source = ""
	} else {
		mechanismType = MechanismTypeChained
	}

	exception.Mechanism = &Mechanism{
		Type:             mechanismType,
		ExceptionID:      currentID,
		ParentID:         parentID,
		Source:           source,
		IsExceptionGroup: isExceptionGroup,
	}

	*exceptions = append(*exceptions, exception)

	switch v := err.(type) {
	case interface{ Unwrap() []error }:
		unwrapped := v.Unwrap()
		for i := range unwrapped {
			if unwrapped[i] != nil {
				childSource := fmt.Sprintf("errors[%d]", i)
				convertErrorDFS(unwrapped[i], exceptions, &currentID, childSource, visited)
			}
		}
	case interface{ Unwrap() error }:
		unwrapped := v.Unwrap()
		if unwrapped != nil {
			unwrappedTypeStr := reflect.TypeOf(unwrapped).String()
			currentTypeStr := reflect.TypeOf(err).String()
			// This specifically catches cases like go-errors.New() where the error wraps a string
			if unwrapped.Error() == err.Error() && unwrappedTypeStr == "*errors.errorString" && currentTypeStr == "*errors.Error" {
				exception.Mechanism.IsExceptionGroup = false
			} else {
				convertErrorDFS(unwrapped, exceptions, &currentID, MechanismSourceCause, visited)
			}
		}
	case interface{ Cause() error }:
		cause := v.Cause()
		if cause != nil {
			convertErrorDFS(cause, exceptions, &currentID, MechanismSourceCause, visited)
		}
	}
}
