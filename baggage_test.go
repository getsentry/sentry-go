package sentry

import "testing"

func TestMergeBaggage(t *testing.T) {
	t.Run("both empty", func(t *testing.T) {
		got, err := MergeBaggage("", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "" {
			t.Fatalf("expected empty baggage, got %q", got)
		}
	})

	t.Run("empty existing returns sentry baggage", func(t *testing.T) {
		got, err := MergeBaggage("", "sentry-trace_id=123,sentry-sampled=true")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		assertBaggageStringsEqual(t, got, "sentry-trace_id=123,sentry-sampled=true")
	})

	t.Run("empty sentry returns existing baggage", func(t *testing.T) {
		got, err := MergeBaggage("othervendor=bla", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		assertBaggageStringsEqual(t, got, "othervendor=bla")
	})

	t.Run("preserves third party members", func(t *testing.T) {
		got, err := MergeBaggage("othervendor=bla", "sentry-trace_id=123,sentry-sampled=true")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		assertBaggageStringsEqual(t, got, "othervendor=bla,sentry-trace_id=123,sentry-sampled=true")
	})

	t.Run("sentry members override existing members", func(t *testing.T) {
		got, err := MergeBaggage(
			"othervendor=bla,sentry-trace_id=old,sentry-sampled=false",
			"sentry-trace_id=new,sentry-sampled=true",
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		assertBaggageStringsEqual(t, got, "othervendor=bla,sentry-trace_id=new,sentry-sampled=true")
	})

	t.Run("invalid existing returns sentry baggage", func(t *testing.T) {
		got, err := MergeBaggage("not-valid", "sentry-trace_id=123,sentry-sampled=true")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		assertBaggageStringsEqual(t, got, "sentry-trace_id=123,sentry-sampled=true")
	})

	t.Run("invalid sentry returns empty and error", func(t *testing.T) {
		got, err := MergeBaggage("othervendor=bla", "sentry-trace_id=123,invalid member,sentry-sampled=true")
		if err == nil {
			t.Fatal("expected error")
		}
		if got != "" {
			t.Fatalf("expected empty baggage, got %q", got)
		}
	})

	t.Run("invalid existing with empty sentry still errors", func(t *testing.T) {
		got, err := MergeBaggage("not-valid", "")
		if err == nil {
			t.Fatal("expected error")
		}
		if got != "" {
			t.Fatalf("expected empty baggage, got %q", got)
		}
	})
}
