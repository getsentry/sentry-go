package sentry

import (
	"testing"
)

func TestPushScopeReturnsCurrentScope(t *testing.T) {
	initialScope := CurrentHub().Scope()
	pushedScope := PushScope()
	defer PopScope()

	pushedScope.SetTag("foo", "bar")

	assertEqual(t, initialScope.tags["foo"], "")
	assertEqual(t, pushedScope.tags["foo"], "bar")
}
