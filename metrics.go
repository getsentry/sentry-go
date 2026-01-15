package sentry

import (
	"context"
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

// NewMeter returns a new Meter associated with the given context.
// If there is no Client associated with the context, or if metrics are disabled,
// it returns a no-op Meter that discards all metrics.
func NewMeter(ctx context.Context) Meter { // nolint:dupl
	var hub *Hub
	hub = GetHubFromContext(ctx)
	if hub == nil {
		hub = CurrentHub()
	}

	client := hub.Client()
	if client != nil && !client.options.DisableMetrics {
		// build default attrs
		serverAddr := client.options.ServerName
		if serverAddr == "" {
			serverAddr, _ = os.Hostname()
		}

		defaults := map[string]string{
			"sentry.release":        client.options.Release,
			"sentry.environment":    client.options.Environment,
			"sentry.server.address": serverAddr,
			"sentry.sdk.name":       client.sdkIdentifier,
			"sentry.sdk.version":    client.sdkVersion,
		}

		defaultAttrs := make(map[string]Attribute)
		for k, v := range defaults {
			if v != "" {
				defaultAttrs[k] = Attribute{Value: v, Type: AttributeString}
			}
		}

		return &sentryMeter{
			hub:               hub,
			client:            client,
			attributes:        make(map[string]Attribute),
			defaultAttributes: defaultAttrs,
			mu:                sync.RWMutex{},
		}
	}

	debuglog.Println("fallback to noopMeter: enableMetrics disabled")
	return &noopMeter{}
}

type sentryMeter struct {
	hub               *Hub
	client            *Client
	attributes        map[string]Attribute
	defaultAttributes map[string]Attribute
	mu                sync.RWMutex
}

func (m *sentryMeter) emit(metricType MetricType, name string, value float64, unit string, attributes map[string]Attribute, customScope *Scope) {
	if name == "" {
		debuglog.Println("empty name provided, dropping metric")
		return
	}

	var traceID TraceID
	var spanID SpanID
	var span *Span
	var user User

	scope := customScope
	if scope == nil {
		scope = m.hub.Scope()
	}

	if scope != nil {
		scope.mu.Lock()
		span = scope.span
		if span != nil {
			traceID = span.TraceID
			spanID = span.SpanID
		} else {
			traceID = scope.propagationContext.TraceID
		}
		user = scope.user
		scope.mu.Unlock()
	}

	// attribute precedence: default -> user -> entry attrs (from SetAttributes) -> call specific
	attrs := make(map[string]Attribute)
	for k, v := range m.defaultAttributes {
		attrs[k] = v
	}

	if !user.IsEmpty() {
		if user.ID != "" {
			attrs["user.id"] = Attribute{Value: user.ID, Type: AttributeString}
		}
		if user.Name != "" {
			attrs["user.name"] = Attribute{Value: user.Name, Type: AttributeString}
		}
		if user.Email != "" {
			attrs["user.email"] = Attribute{Value: user.Email, Type: AttributeString}
		}
	}

	m.mu.RLock()
	for k, v := range m.attributes {
		attrs[k] = v
	}
	m.mu.RUnlock()

	for k, v := range attributes {
		attrs[k] = v
	}

	metric := &Metric{
		Timestamp:  time.Now(),
		TraceID:    traceID,
		SpanID:     spanID,
		Type:       metricType,
		Name:       name,
		Value:      value,
		Unit:       unit,
		Attributes: attrs,
	}

	if m.client.options.BeforeSendMetric != nil {
		metric = m.client.options.BeforeSendMetric(metric)
	}

	if metric != nil {
		if m.client.telemetryBuffer != nil {
			if !m.client.telemetryBuffer.Add(metric) {
				debuglog.Printf("Dropping event: metric buffer full or category missing")
			}
		} else if m.client.batchMeter != nil {
			m.client.batchMeter.Send(metric)
		}
	}

	if m.client.options.Debug {
		debuglog.Printf("Metric %s [%s]: %f %s", metricType, name, value, unit)
	}
}

// Count implements Meter.
func (m *sentryMeter) Count(name string, count int64, options MeterOptions) {
	attrs := make(map[string]Attribute)
	if options.Attributes != nil {
		for _, attr := range options.Attributes {
			t, ok := mapTypesToStr[attr.Value.Type()]
			if !ok || t == "" {
				debuglog.Printf("invalid attribute type set: %v", t)
				continue
			}
			attrs[attr.Key] = Attribute{Value: attr.Value.AsInterface(), Type: t}
		}
	}

	m.emit(MetricTypeCounter, name, float64(count), "", attrs, options.Scope)
}

// Distribution implements Meter.
func (m *sentryMeter) Distribution(name string, sample float64, options MeterOptions) {
	attrs := make(map[string]Attribute)
	if options.Attributes != nil {
		for _, attr := range options.Attributes {
			t, ok := mapTypesToStr[attr.Value.Type()]
			if !ok || t == "" {
				debuglog.Printf("invalid attribute type set: %v", t)
				continue
			}
			attrs[attr.Key] = Attribute{Value: attr.Value.AsInterface(), Type: t}
		}
	}

	m.emit(MetricTypeDistribution, name, sample, options.Unit, attrs, options.Scope)
}

// Gauge implements Meter.
func (m *sentryMeter) Gauge(name string, value float64, options MeterOptions) {
	attrs := make(map[string]Attribute)
	if options.Attributes != nil {
		for _, attr := range options.Attributes {
			t, ok := mapTypesToStr[attr.Value.Type()]
			if !ok || t == "" {
				debuglog.Printf("invalid attribute type set: %v", t)
				continue
			}
			attrs[attr.Key] = Attribute{Value: attr.Value.AsInterface(), Type: t}
		}
	}

	m.emit(MetricTypeGauge, name, value, options.Unit, attrs, options.Scope)
}

// SetAttributes implements Meter.
func (m *sentryMeter) SetAttributes(attrs ...attribute.Builder) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, v := range attrs {
		t, ok := mapTypesToStr[v.Value.Type()]
		if !ok || t == "" {
			debuglog.Printf("invalid attribute type set: %v", t)
			continue
		}

		m.attributes[v.Key] = Attribute{
			Value: v.Value.AsInterface(),
			Type:  t,
		}
	}
}

// noopMeter is a no-operation implementation of Meter.
// This is used when there is no client available in the context.
type noopMeter struct{}

// Count implements Meter.
func (n *noopMeter) Count(_ string, _ int64, _ MeterOptions) {
}

// Distribution implements Meter.
func (n *noopMeter) Distribution(_ string, _ float64, _ MeterOptions) {
}

// Gauge implements Meter.
func (n *noopMeter) Gauge(_ string, _ float64, _ MeterOptions) {
}

// GetCtx implements Meter.
func (n *noopMeter) GetCtx() context.Context {
	return context.Background()
}

// SetAttributes implements Meter.
func (n *noopMeter) SetAttributes(...attribute.Builder) {
}
