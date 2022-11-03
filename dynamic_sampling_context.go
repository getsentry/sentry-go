package sentry

import (
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

func NewDynamicSamplingContext(header []byte) (DynamicSamplingContext, error) {
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

func (d DynamicSamplingContext) HasEntries() bool {
	return len(d.Entries) > 0
}

func (d DynamicSamplingContext) String() string {
	return ""
}
