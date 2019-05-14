package sentryintegrations

import (
	"os"
)

func fileExists(fileName string) bool {
	if _, err := os.Stat(fileName); err != nil {
		return false
	}

	return true
}
