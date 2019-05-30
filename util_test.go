package sentry

import (
	"testing"
)

func TestUUIDReturnsRandom32CharacterString(t *testing.T) {
	u1 := uuid()
	u2 := uuid()
	u3 := uuid()

	assertEqual(t, len(u1), 32)
	assertEqual(t, len(u2), 32)
	assertEqual(t, len(u3), 32)

	assertNotEqual(t, u1, u2)
	assertNotEqual(t, u1, u3)
	assertNotEqual(t, u2, u3)
}

func TestFileExistsReturnsTrueForExistingFiles(t *testing.T) {
	assertEqual(t, fileExists(("util.go")), true)
	assertEqual(t, fileExists(("util_test.go")), true)
}

func TestFileExistsReturnsFalseForNonExistingFiles(t *testing.T) {
	assertEqual(t, fileExists(("util_nope.go")), false)
	assertEqual(t, fileExists(("util_nope_test.go")), false)
}
