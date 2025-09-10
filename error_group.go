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
	if err == nil {
		return nil
	}

	var exceptions []Exception
	visited := make(map[error]bool)
	convertErrorDFS(err, &exceptions, nil, "", visited)

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
	case interface{ Unwrap() []error }:
		isExceptionGroup = true
	case interface{ Unwrap() error }:
		isExceptionGroup = true
	case interface{ Cause() error }:
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
			convertErrorDFS(unwrapped, exceptions, &currentID, MechanismSourceCause, visited)
		}
	case interface{ Cause() error }:
		cause := v.Cause()
		if cause != nil {
			convertErrorDFS(cause, exceptions, &currentID, MechanismSourceCause, visited)
		}
	}
}
