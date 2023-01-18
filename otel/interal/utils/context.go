package utils

import (
	crypto_rand "crypto/rand"
	"fmt"
)

// TODO(anton): Do we actually need all this? Take a look at spanContextKey in tracing.go

type CtxKey string

// ContextKey generates a unique values suitable as a key for context.WithValue()
// and ctx.Value().
func contextKey(prefix string) CtxKey {
	const suffixLength = 16
	b := make([]byte, suffixLength)
	// "crypto/rand" doesn't need RNG seeding
	crypto_rand.Read(b)
	return CtxKey(fmt.Sprintf("%s-%x", prefix, b))
}

var dynamicSamplingContextKey CtxKey

func DynamicSamplingContextKey() CtxKey {
	if dynamicSamplingContextKey == "" {
		dynamicSamplingContextKey = contextKey("DynamicSamplingContextKey")
	}
	return dynamicSamplingContextKey
}

var sentryTraceContextKey CtxKey

func SentryTraceContextKey() CtxKey {
	if sentryTraceContextKey == "" {
		sentryTraceContextKey = contextKey("SentryTraceContextKey")
	}
	return sentryTraceContextKey
}

var sentryTraceHeaderKey CtxKey

func SentryTraceHeaderKey() CtxKey {
	if sentryTraceHeaderKey == "" {
		sentryTraceHeaderKey = contextKey("SentryTraceHeaderKey")
	}
	return sentryTraceHeaderKey
}

var sentryBaggageHeaderKey CtxKey

func SentryBaggageHeaderKey() CtxKey {
	if sentryBaggageHeaderKey == "" {
		sentryBaggageHeaderKey = contextKey("SentryBaggageHeaderKey")
	}
	return sentryBaggageHeaderKey
}
