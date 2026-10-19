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
		// If there's at least one Sentry value, we consider the DSC frozen
		Frozen: len(entries) > 0,
	}, nil
}

func DynamicSamplingContextFromTransaction(span *Span) DynamicSamplingContext {
	if span == nil {
		return DynamicSamplingContext{}
	}
	return span.dynamicSamplingContextForPropagation()
}

func dynamicSamplingContextFromTransaction(span *Span, client *Client) DynamicSamplingContext {
	if !client.IsEnabled() {
		return DynamicSamplingContext{
			Entries: map[string]string{},
			Frozen:  false,
		}
	}

	entries := make(map[string]string)

	if traceID := span.TraceID.String(); traceID != "" {
		entries[traceIDContextKey] = traceID
	}
	if client.options.EnableTracing && span.sampleRate >= 0 {
		entries["sample_rate"] = strconv.FormatFloat(span.sampleRate, 'f', -1, 64)
	}

	if dsn := client.dsn; dsn != nil {
		if publicKey := dsn.GetPublicKey(); publicKey != "" {
			entries["public_key"] = publicKey
		}
		if orgID := dsn.GetOrgID(); orgID != 0 {
			entries["org_id"] = strconv.FormatUint(orgID, 10)
		}
	}
	if release := client.options.Release; release != "" {
		entries["release"] = release
	}
	if environment := client.options.Environment; environment != "" {
		entries["environment"] = environment
	}

	// Only include the transaction name if it's of good quality (not empty and not SourceURL)
	if client.options.EnableTracing && span.Source != "" && span.Source != SourceURL {
		if span.IsTransaction() {
			entries["transaction"] = span.Name
		}
	}

	if client.options.EnableTracing {
		entries["sampled"] = strconv.FormatBool(span.Sampled.Bool())
	}

	return DynamicSamplingContext{Entries: entries, Frozen: true}
}

func (d DynamicSamplingContext) HasEntries() bool {
	return len(d.Entries) > 0
}

func (d DynamicSamplingContext) IsFrozen() bool {
	return d.Frozen
}

func (d DynamicSamplingContext) String() string {
	members := []baggage.Member{}
	for k, entry := range d.Entries {
		member, err := baggage.NewMember(sentryPrefix+k, entry)
		if err != nil {
			continue
		}
		members = append(members, member)
	}

	if len(members) == 0 {
		return ""
	}

	baggage, err := baggage.New(members...)
	if err != nil {
		return ""
	}

	return baggage.String()
}

// DynamicSamplingContextFromScope returns the scope's propagation DSC.
// It is safe for concurrent use and freezes an unfrozen scope DSC on first use.
func DynamicSamplingContextFromScope(scope *Scope, client *Client) DynamicSamplingContext {
	if scope == nil || !client.IsEnabled() {
		return DynamicSamplingContext{
			Entries: map[string]string{},
			Frozen:  false,
		}
	}
	return scope.propagationContextForPropagation(client).DynamicSamplingContext
}

func dynamicSamplingContextFromPropagationContext(
	propagationContext PropagationContext,
	client *Client,
) DynamicSamplingContext {
	entries := map[string]string{}
	if traceID := propagationContext.TraceID.String(); traceID != "" {
		entries[traceIDContextKey] = traceID
	}
	if rate := client.options.TracesSampleRate; client.options.EnableTracing && client.options.TracesSampler == nil && rate >= 0 && rate <= 1 {
		entries["sample_rate"] = strconv.FormatFloat(rate, 'f', -1, 64)
	}
	if dsn := client.dsn; dsn != nil {
		if publicKey := dsn.GetPublicKey(); publicKey != "" {
			entries["public_key"] = publicKey
		}
		if orgID := dsn.GetOrgID(); orgID != 0 {
			entries["org_id"] = strconv.FormatUint(orgID, 10)
		}
	}
	if release := client.options.Release; release != "" {
		entries["release"] = release
	}
	if environment := client.options.Environment; environment != "" {
		entries["environment"] = environment
	}

	return DynamicSamplingContext{
		Entries: entries,
		Frozen:  true,
	}
}

func frozenDSCMatchesTrace(dsc DynamicSamplingContext, traceID TraceID) bool {
	return !dsc.IsFrozen() || dsc.Entries[traceIDContextKey] == "" || strings.EqualFold(dsc.Entries[traceIDContextKey], traceID.String())
}
