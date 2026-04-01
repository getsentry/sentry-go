package sentry

import (
	"fmt"

	"github.com/getsentry/sentry-go/internal/otel/baggage"
)

// MergeBaggage merges an existing baggage header with a Sentry-generated one.
//
// Existing third-party members are preserved. If both baggage strings contain
// the same member key, the Sentry-generated member wins.
func MergeBaggage(existingHeader, sentryHeader string) (string, error) {
	// TODO: we are reparsing the headers here, because we currently don't
	// expose a method to get only DSC or its baggage members.
	sentryBaggage, err := baggage.Parse(sentryHeader)
	if err != nil {
		return "", fmt.Errorf("cannot parse sentryHeader: %w", err)
	}

	finalBaggage, err := baggage.Parse(existingHeader)
	if err != nil {
		return "", fmt.Errorf("cannot parse existingHeader: %w", err)
	}

	for _, member := range sentryBaggage.Members() {
		finalBaggage, err = finalBaggage.SetMember(member)
		if err != nil {
			return "", fmt.Errorf("cannot merge baggage: %w", err)
		}
	}

	return finalBaggage.String(), nil
}
