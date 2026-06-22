package sentry

import "testing"

func TestOptionZeroValueIsUnset(t *testing.T) {
	t.Parallel()

	var option Option[bool]
	if option.IsSet {
		t.Fatal("zero-value option should be unset")
	}
	if got := option.Or(true); got != true {
		t.Errorf("unset option should return default, got %v", got)
	}
}

func TestSetReturnsSetOption(t *testing.T) {
	t.Parallel()

	option := Set(false)
	if !option.IsSet {
		t.Fatal("Set should mark option as set")
	}
	if option.Value {
		t.Fatal("Set should preserve explicitly configured false")
	}
	if got := option.Or(true); got != false {
		t.Errorf("set option should return explicit value, got %v", got)
	}
}
