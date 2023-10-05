//go:build !go1.20

package sentry

import "strings"

// In versions of Go below 1.20 a prefix of "type." and "go." is a
// compiler-generated symbol that doesn't belong to any package.
// See variable reservedimports in cmd/compile/internal/gc/subr.go
func isCompilerGeneratedSymbol(name string) bool {
	if strings.HasPrefix(name, "go.") || strings.HasPrefix(name, "type.") {
		return true
	}
	return false
}
