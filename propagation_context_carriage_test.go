package sentry

import (
	"context"
	"testing"
	"time"
)

func TestContinueTraceCarriesPropagationOnDerivedContext(t *testing.T) {
	parent, cancel := context.WithTimeout(context.WithValue(context.Background(), testContextKey{}, testContextValue{}), time.Minute)
	defer cancel()
	parent, scope := ScopeFromContext(parent)
	before := scope.propagationContextSnapshot()

	ctx := ContinueTrace(parent,
		"d49d9bf66f13450b81f65bc51cf49c03-a9f442f9330b4e09-1",
		"sentry-release=1.2.3,sentry-sample_rate=0.5",
	)
	if ctx == parent {
		t.Fatal("ContinueTrace returned the parent context")
	}
	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("ContinueTrace did not preserve parent deadline")
	}
	if _, ok := ctx.Value(testContextKey{}).(testContextValue); !ok {
		t.Fatal("ContinueTrace did not preserve parent value")
	}

	propagation, ok := propagationContextFromContext(ctx)
	if !ok {
		t.Fatal("continued context has no propagation state")
	}
	if propagation.TraceID != TraceIDFromHex("d49d9bf66f13450b81f65bc51cf49c03") ||
		propagation.ParentSpanID != SpanIDFromHex("a9f442f9330b4e09") ||
		propagation.Sampled != SampledTrue {
		t.Fatalf("unexpected propagation state: %+v", propagation)
	}
	if propagation.DynamicSamplingContext.Entries["release"] != "1.2.3" {
		t.Fatalf("unexpected DSC: %+v", propagation.DynamicSamplingContext)
	}
	if after := scope.propagationContextSnapshot(); after.TraceID != before.TraceID || after.SpanID != before.SpanID || after.ParentSpanID != before.ParentSpanID {
		t.Fatal("ContinueTrace mutated Scope propagation state")
	}
}

func TestPropagationDSCDoesNotAlias(t *testing.T) {
	ctx := ContinueTrace(context.Background(),
		"d49d9bf66f13450b81f65bc51cf49c03-a9f442f9330b4e09-1",
		"sentry-release=parent",
	)
	first, ok := propagationContextFromContext(ctx)
	if !ok {
		t.Fatal("missing propagation state")
	}
	first.DynamicSamplingContext.Entries["release"] = "mutated"
	second, _ := propagationContextFromContext(ctx)
	if second.DynamicSamplingContext.Entries["release"] != "parent" {
		t.Fatal("propagation reads alias DSC entries")
	}

	isolated := WithIsolation(ctx)
	isolatedPropagation, _ := propagationContextFromContext(isolated)
	isolatedPropagation.DynamicSamplingContext.Entries["release"] = "child"
	again, _ := propagationContextFromContext(ctx)
	if again.DynamicSamplingContext.Entries["release"] != "parent" {
		t.Fatal("WithIsolation propagation aliases DSC entries")
	}
}

func TestIsolationAndStartNewTraceCreateFreshPropagation(t *testing.T) {
	first := WithIsolation(context.Background())
	second := WithIsolation(context.Background())
	firstPropagation, firstOK := propagationContextFromContext(first)
	secondPropagation, secondOK := propagationContextFromContext(second)
	if !firstOK || !secondOK || firstPropagation.TraceID == secondPropagation.TraceID {
		t.Fatal("independent isolation boundaries must receive distinct propagation")
	}

	child := StartNewTrace(first)
	childPropagation, ok := propagationContextFromContext(child)
	if !ok || childPropagation.TraceID == firstPropagation.TraceID {
		t.Fatal("StartNewTrace did not derive distinct propagation")
	}
	parentAgain, _ := propagationContextFromContext(first)
	if parentAgain.TraceID != firstPropagation.TraceID {
		t.Fatal("StartNewTrace mutated parent propagation")
	}
}

func TestContextHeadersUsePropagationWithoutSpanOrHub(t *testing.T) {
	ctx := ContinueTrace(context.Background(),
		"d49d9bf66f13450b81f65bc51cf49c03-a9f442f9330b4e09-0",
		"sentry-release=1.2.3",
	)
	propagation, ok := propagationContextFromContext(ctx)
	if !ok {
		t.Fatal("missing propagation state")
	}
	if got, want := GetTraceparent(ctx), "d49d9bf66f13450b81f65bc51cf49c03-"+propagation.SpanID.String()+"-0"; got != want {
		t.Fatalf("GetTraceparent = %q, want %q", got, want)
	}
	if got, want := GetTraceparentW3C(ctx), "00-d49d9bf66f13450b81f65bc51cf49c03-"+propagation.SpanID.String()+"-00"; got != want {
		t.Fatalf("GetTraceparentW3C = %q, want %q", got, want)
	}
	if got := GetBaggage(ctx); got == "" {
		t.Fatal("GetBaggage returned empty DSC")
	}

	hubScope := NewScope()
	hubScope.SetPropagationContext(PropagationContext{TraceID: TraceIDFromHex("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")})
	hubCtx := SetHubOnContext(context.Background(), NewHub(NewNoopClient(), hubScope))
	if got := GetTraceparent(hubCtx); got != "" {
		t.Fatalf("context header helper fell back to Hub: %q", got)
	}
}
