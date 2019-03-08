package sentry

import "testing"

func TestSentry(t *testing.T) {
	got := Sentry()
	want := "Sentry Go"

	if got != want {
		t.Errorf("got '%s' want '%s'", got, want)
	}
}
