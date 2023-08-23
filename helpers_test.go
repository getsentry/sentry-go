package sentry

import (
	"github.com/getsentry/sentry-go/internal/testutils"
	"os"
)

var assertEqual = testutils.AssertEqual
var assertNotEqual = testutils.AssertNotEqual
var assertBaggageStringsEqual = testutils.AssertBaggageStringsEqual

func envSetter(envs map[string]string) (closer func()) {
	originalEnvs := map[string]string{}

	for name, value := range envs {
		if originalValue, ok := os.LookupEnv(name); ok {
			originalEnvs[name] = originalValue
		}
		_ = os.Setenv(name, value)
	}

	return func() {
		for name := range envs {
			origValue, has := originalEnvs[name]
			if has {
				_ = os.Setenv(name, origValue)
			} else {
				_ = os.Unsetenv(name)
			}
		}
	}
}
