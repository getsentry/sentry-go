//go:build race

package testutils

// IsRaceTest returns true when the test is run with the race detector enabled.
func IsRaceTest() bool {
	return true
}
