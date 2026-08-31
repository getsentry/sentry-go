package sentry

import (
	"context"
	"maps"
	"os"
	"sync"
	"time"

	"github.com/getsentry/sentry-go/attribute"
	"github.com/getsentry/sentry-go/internal/debuglog"
)

// Duration Units.
const (
	UnitNanosecond  = "nanosecond"
	UnitMicrosecond = "microsecond"
	UnitMillisecond = "millisecond"
	UnitSecond      = "second"
	UnitMinute      = "minute"
	UnitHour        = "hour"
	UnitDay         = "day"
	UnitWeek        = "week"
)

// Information Units.
const (
	UnitBit      = "bit"
	UnitByte     = "byte"
	UnitKilobyte = "kilobyte"
	UnitKibibyte = "kibibyte"
	UnitMegabyte = "megabyte"
	UnitMebibyte = "mebibyte"
	UnitGigabyte = "gigabyte"
	UnitGibibyte = "gibibyte"
	UnitTerabyte = "terabyte"
	UnitTebibyte = "tebibyte"
	UnitPetabyte = "petabyte"
	UnitPebibyte = "pebibyte"
	UnitExabyte  = "exabyte"
	UnitExbibyte = "exbibyte"
)

// Fraction Units.
const (
	UnitRatio   = "ratio"
	UnitPercent = "percent"
)

// NewMeter returns a new Meter. If there is no enabled Client available from the current context,
// it returns a no-op Meter that discards all metrics.
func NewMeter(ctx context.Context) Meter {
	client := GetClient(ctx)
	options := client.options
	if client.IsEnabled() {
		// build default attrs
		serverAddr := options.ServerName
		if serverAddr == "" {
			serverAddr, _ = os.Hostname()
		}

		defaults := map[string]string{
			"sentry.release":        options.Release,
			"sentry.environment":    options.Environment,
			"sentry.server.address": serverAddr,
			"sentry.sdk.name":       client.GetSDKIdentifier(),
			"sentry.sdk.version":    client.GetSDKVersion(),
		}

		defaultAttrs := make(map[string]attribute.Value)
		for k, v := range defaults {
			if v != "" {
				defaultAttrs[k] = attribute.StringValue(v)
			}
		}

		return &sentryMeter{
			ctx:               ctx,
			attributes:        make(map[string]attribute.Value),
			defaultAttributes: defaultAttrs,
			mu:                sync.RWMutex{},
		}
	}

	debuglog.Printf("fallback to noopMeter: SDK not initialized")
	return &noopMeter{}
}

type sentryMeter struct {
	ctx               context.Context
	attributes        map[string]attribute.Value
	defaultAttributes map[string]attribute.Value
	mu                sync.RWMutex
}

func (m *sentryMeter) emit(ctx context.Context, metricType MetricType, name string, value MetricValue, unit string, attributes []attribute.Builder, customScope *Scope) {
	if name == "" {
		debuglog.Println("empty name provided, dropping metric")
		return
	}

	client := GetClient(ctx)
	scope := ScopeFromContext(ctx)
	if customScope != nil {
		scope = customScope
	}
	m.mu.RLock()
	attrs := buildMetricAttributes(attributes, m.attributes)
	m.mu.RUnlock()

	metric := &Metric{
		Timestamp:  time.Now(),
		Type:       metricType,
		Name:       name,
		Value:      value,
		Unit:       unit,
		Attributes: attrs,
	}

	if client.captureMetric(metric, signalCaptureContext{
		scope:             scope,
		ctx:               ctx,
		defaultAttributes: m.defaultAttributes,
	}) && client.options.Debug {
		debuglog.Printf("Metric %s [%s]: %v %s", metricType, name, value.AsInterface(), unit)
	}
}

func prepareMetric(metric *Metric, client *Client, capture signalCaptureContext) {
	metric.TraceID, metric.SpanID = traceIDsFromContext(capture.ctx, capture.scope, client)
	metric.Attributes = mergeScopeAttributes(metric.Attributes, capture.defaultAttributes, capture.scope)
}

// WithCtx returns a new Meter that uses the given context for trace/span association.
func (m *sentryMeter) WithCtx(ctx context.Context) Meter {
	m.mu.RLock()
	attrsCopy := maps.Clone(m.attributes)
	m.mu.RUnlock()

	return &sentryMeter{
		ctx:               ctx,
		attributes:        attrsCopy,
		defaultAttributes: m.defaultAttributes,
		mu:                sync.RWMutex{},
	}
}

func (m *sentryMeter) applyOptions(opts []MeterOption) *meterOptions {
	o := &meterOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// Count implements Meter.
func (m *sentryMeter) Count(name string, count int64, opts ...MeterOption) {
	o := m.applyOptions(opts)
	m.emit(m.ctx, MetricTypeCounter, name, Int64MetricValue(count), o.unit, o.attributes, o.scope)
}

// Distribution implements Meter.
func (m *sentryMeter) Distribution(name string, sample float64, opts ...MeterOption) {
	o := m.applyOptions(opts)
	m.emit(m.ctx, MetricTypeDistribution, name, Float64MetricValue(sample), o.unit, o.attributes, o.scope)
}

// Gauge implements Meter.
func (m *sentryMeter) Gauge(name string, value float64, opts ...MeterOption) {
	o := m.applyOptions(opts)
	m.emit(m.ctx, MetricTypeGauge, name, Float64MetricValue(value), o.unit, o.attributes, o.scope)
}

// SetAttributes implements Meter.
func (m *sentryMeter) SetAttributes(attrs ...attribute.Builder) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, a := range attrs {
		if a.Value.Type() == attribute.INVALID {
			debuglog.Printf("invalid attribute: %v", a)
			continue
		}
		m.attributes[a.Key] = a.Value
	}
}

// noopMeter is a no-operation implementation of Meter.
// This is used when there is no client available in the context or when metrics are disabled.
type noopMeter struct{}

// WithCtx implements Meter.
func (n *noopMeter) WithCtx(_ context.Context) Meter {
	return n
}

// Count implements Meter.
func (n *noopMeter) Count(name string, _ int64, _ ...MeterOption) {
	debuglog.Printf("Metric %q is being dropped. Ensure the SDK is initialized", name)
}

// Distribution implements Meter.
func (n *noopMeter) Distribution(name string, _ float64, _ ...MeterOption) {
	debuglog.Printf("Metric %q is being dropped. Ensure the SDK is initialized", name)
}

// Gauge implements Meter.
func (n *noopMeter) Gauge(name string, _ float64, _ ...MeterOption) {
	debuglog.Printf("Metric %q is being dropped. Ensure the SDK is initialized", name)
}

// SetAttributes implements Meter.
func (n *noopMeter) SetAttributes(_ ...attribute.Builder) {
	debuglog.Printf("No attributes attached")
}
