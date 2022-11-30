package sentry

import (
	"strconv"
	"strings"

	"github.com/getsentry/sentry-go/internal/otel/baggage"
)

const (
	sentryPrefix = "sentry-"
)

// DynamicSamplingContext holds information about the current event that can be used to make dynamic sampling decisions.
type DynamicSamplingContext struct {
	Entries map[string]string
	Frozen  bool
}

func DynamicSamplingContextFromHeader(header []byte) (DynamicSamplingContext, error) {
	bag, err := baggage.Parse(string(header))
	if err != nil {
		return DynamicSamplingContext{}, err
	}

	entries := map[string]string{}
	for _, member := range bag.Members() {
		// We only store baggage members if their key starts with "sentry-".
		if k, v := member.Key(), member.Value(); strings.HasPrefix(k, sentryPrefix) {
			entries[strings.TrimPrefix(k, sentryPrefix)] = v
		}
	}

	return DynamicSamplingContext{
		Entries: entries,
		Frozen:  true,
	}, nil
}

func DynamicSamplingContextFromTransaction(span *Span) (DynamicSamplingContext, error) {
	entries := map[string]string{}

	hub := hubFromContext(span.Context())
	scope := hub.Scope()
	options := hub.Client().Options()

	if traceID := span.TraceID.String(); traceID != "" {
		entries["trace_id"] = traceID
	}
	if sampleRate := span.sampleRate; sampleRate != 0 {
		entries["sample_rate"] = strconv.FormatFloat(sampleRate, 'f', -1, 64)
	}

	if dsn := hub.Client().dsn; dsn != nil {
		if publicKey := dsn.publicKey; publicKey != "" {
			entries["public_key"] = publicKey
		}
	}
	if release := options.Release; release != "" {
		entries["release"] = release
	}
	if environment := options.Environment; environment != "" {
		entries["environment"] = environment
	}

	// Only include the transaction name if it's of good quality (not empty and not SourceURL)
	if span.Source != "" && span.Source != SourceURL {
		if transactioName := scope.Transaction(); transactioName != "" {
			entries["transaction"] = transactioName
		}
	}

	if userSegment := scope.user.Segment; userSegment != "" {
		entries["user_segment"] = userSegment
	}

	return DynamicSamplingContext{
		Entries: entries,
		Frozen:  true,
	}, nil
}

func (d DynamicSamplingContext) HasEntries() bool {
	return len(d.Entries) > 0
}

func (d DynamicSamplingContext) IsFrozen() bool {
	return d.Frozen
}

func (d DynamicSamplingContext) String() string {
	// TODO implement me
	return ""
}
