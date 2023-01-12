package utils

import (
	crypto_rand "crypto/rand"
	"fmt"
)

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
	if dynamicSamplingContextKey == "" {
		sentryTraceContextKey = contextKey("SentryTraceContextKey")
	}
	return sentryTraceContextKey
}
