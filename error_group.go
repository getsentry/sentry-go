package sentry

import (
	"errors"
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
	if err == nil {
		return nil
	}

	var exceptions []Exception
	convertErrorDFS(err, &exceptions, nil, "")

	// For single exceptions, remove the mechanism
	if len(exceptions) == 1 {
		exceptions[0].Mechanism = nil
	}

	// Reverse the array so root exception (ID 0) is at the end
	slices.Reverse(exceptions)

	return exceptions
}

func convertErrorDFS(err error, exceptions *[]Exception, parentID *int, source string) {
	if err == nil {
		return
	}

	_, isExceptionGroup := err.(interface{ Unwrap() []error })

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
		for i := len(unwrapped) - 1; i >= 0; i-- {
			if unwrapped[i] != nil {
				childSource := fmt.Sprintf("errors[%d]", i)
				convertErrorDFS(unwrapped[i], exceptions, &currentID, childSource)
			}
		}
	case interface{ Unwrap() error }:
		unwrapped := v.Unwrap()
		if unwrapped != nil {
			convertErrorDFS(unwrapped, exceptions, &currentID, MechanismSourceCause)
		}
	case interface{ Cause() error }:
		cause := v.Cause()
		if cause != nil && !errors.Is(cause, err) { // Avoid infinite recursion
			convertErrorDFS(cause, exceptions, &currentID, MechanismSourceCause)
		}
	}
}
