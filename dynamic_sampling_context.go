package sentry

import (
	"encoding/binary"
	"math"
	"strconv"
	"strings"

	"github.com/getsentry/sentry-go/internal/otel/baggage"
)

const (
	sentryPrefix  = "sentry-"
	sampleRandKey = "sample_rand"
)

// sampleRandScale is the number of values a sample_rand can take: it has six
// decimal places.
const sampleRandScale = 1_000_000

// deriveSampleRand returns a value in [lo, hi) millionths, taken from the 56
// trace ID bits that W3C Trace Context defines as random.
func deriveSampleRand(traceID TraceID, lo, hi uint64) float64 {
	random := binary.BigEndian.Uint64(traceID[8:]) & (1<<56 - 1)
	return float64(lo+random%(hi-lo)) / sampleRandScale
}

func formatSampleRand(sampleRand float64) string {
	return strconv.FormatFloat(sampleRand, 'f', 6, 64)
}

// sampleRand returns the random value every SDK in the trace compares its
// sample rate against: the propagated one, or one derived from the trace ID.
func sampleRand(traceID TraceID, dsc DynamicSamplingContext) float64 {
	if propagated, ok := propagatedSampleRand(dsc); ok {
		return propagated
	}
	return deriveSampleRand(traceID, 0, sampleRandScale)
}

func propagatedSampleRand(dsc DynamicSamplingContext) (float64, bool) {
	sampleRand, err := strconv.ParseFloat(dsc.Entries[sampleRandKey], 64)
	return sampleRand, err == nil && sampleRand >= 0 && sampleRand < 1
}

// backfillSampleRand gives an incoming DSC without a valid sample_rand one that
// agrees with the incoming sampling decision and sample rate.
func backfillSampleRand(trace TraceParentContext, dsc DynamicSamplingContext) {
	if _, ok := propagatedSampleRand(dsc); ok {
		return
	}
	lo, hi := uint64(0), uint64(sampleRandScale)
	if rate, err := strconv.ParseFloat(dsc.Entries["sample_rate"], 64); err == nil && rate >= 0 && rate <= 1 {
		// below counts the values smaller than rate.
		below := uint64(math.Round(rate * sampleRandScale))
		if float64(below)/sampleRandScale < rate {
			below++
		}
		switch {
		case trace.Sampled == SampledTrue && below > 0:
			hi = below
		case trace.Sampled == SampledFalse && below < sampleRandScale:
			lo = below
		}
	}
	dsc.Entries[sampleRandKey] = formatSampleRand(deriveSampleRand(trace.TraceID, lo, hi))
}

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
	entries[sampleRandKey] = formatSampleRand(deriveSampleRand(span.TraceID, 0, sampleRandScale))
	if client.options.tracingEnabled() && span.sampleRate >= 0 && span.sampleRate <= 1 {
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
	if client.options.tracingEnabled() && span.Source != "" && span.Source != SourceURL {
		if span.IsTransaction() {
			entries["transaction"] = span.Name
		}
	}

	if client.options.tracingEnabled() {
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
	entries[sampleRandKey] = formatSampleRand(deriveSampleRand(propagationContext.TraceID, 0, sampleRandScale))
	if rate := client.options.TracesSampleRate; rate != nil && client.options.TracesSampler == nil && *rate >= 0 && *rate <= 1 {
		entries["sample_rate"] = strconv.FormatFloat(*rate, 'f', -1, 64)
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
