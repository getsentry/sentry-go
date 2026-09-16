package sentry

import (
	"fmt"
	"strings"

	"github.com/getsentry/sentry-go/internal/debuglog"
	"github.com/getsentry/sentry-go/internal/otel/baggage"
)

// MergeBaggage merges an existing baggage header with a Sentry-generated one.
//
// Existing third-party members are preserved while stale Sentry members are
// replaced by the generated baggage. The helper is best-effort and only keeps
// the Sentry baggage in case the existing one is malformed.
func MergeBaggage(existingHeader, sentryHeader string) (string, error) {
	// TODO: we are reparsing the headers here, because we currently don't
	// expose a method to get only DSC or its baggage members.
	sentryBaggage, err := baggage.Parse(sentryHeader)
	if err != nil {
		return "", fmt.Errorf("cannot parse sentryHeader: %w", err)
	}

	existingBaggage, err := baggage.Parse(existingHeader)
	if err != nil {
		if sentryBaggage.Len() == 0 {
			return "", fmt.Errorf("cannot parse existingHeader: %w", err)
		}
		// in case that the incoming header is malformed we should only
		// care about merging sentry related baggage information for distributed tracing.
		debuglog.Printf("malformed incoming header: %v", err)
		return sentryBaggage.String(), nil
	}

	parts := make([]string, 0, sentryBaggage.Len()+existingBaggage.Len())
	if s := sentryBaggage.String(); s != "" {
		parts = append(parts, s)
	}
	for _, member := range existingBaggage.Members() {
		if strings.HasPrefix(member.Key(), sentryPrefix) {
			continue
		}
		parts = append(parts, member.String())
	}

	return strings.Join(parts, ","), nil
}
