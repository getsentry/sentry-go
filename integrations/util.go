package sentry

import (
	"io/ioutil"
	"log"
	"os"
)

var debugger = log.New(ioutil.Discard, "[Sentry]", log.LstdFlags)

func fileExists(fileName string) bool {
	if _, err := os.Stat(fileName); err != nil {
		return false
	}

	return true
}
