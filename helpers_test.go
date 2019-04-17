package sentry

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func prettyPrint(v interface{}) {
	pp, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(pp))
}

func assertEqual(t *testing.T, got, want interface{}, userMessage ...interface{}) {
	t.Helper()

	if !reflect.DeepEqual(got, want) {
		logFailedAssertion(t, formatUnequalValues(got, want), userMessage...)
	}
}

func assertNotEqual(t *testing.T, got, want interface{}, userMessage ...interface{}) {
	t.Helper()

	if reflect.DeepEqual(got, want) {
		logFailedAssertion(t, formatUnequalValues(got, want), userMessage...)
	}
}

func assertStringContains(t *testing.T, input, substr string) {
	t.Helper()

	if !strings.Contains(input, substr) {
		logFailedAssertion(t, fmt.Sprintf("expected '%s' to contain '%s'", input, substr))
	}
}

func logFailedAssertion(t *testing.T, summary string, userMessage ...interface{}) {
	t.Helper()
	text := summary

	if len(userMessage) > 0 {
		if message, ok := userMessage[0].(string); ok {
			if message != "" && len(userMessage) > 1 {
				text = fmt.Sprintf(message, userMessage[1:]...) + text
			} else if message != "" {
				text = fmt.Sprint(message) + text
			}
		}
	}

	t.Error(text)
}

func formatUnequalValues(got, want interface{}) string {
	var a, b string

	if reflect.TypeOf(got) != reflect.TypeOf(want) {
		a, b = fmt.Sprintf("%T(%#v)", got, got), fmt.Sprintf("%T(%#v)", want, want)
	} else {
		a, b = fmt.Sprintf("%#v", got), fmt.Sprintf("%#v", want)
	}

	return fmt.Sprintf("\ngot: %s\nwant: %s", a, b)
}
