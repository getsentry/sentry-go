package sentry

import (
	"context"
	"maps"
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

// NewMeter returns a new Meter.
func NewMeter(ctx context.Context) Meter {
	return &sentryMeter{ctx: ctx}
}

type sentryMeter struct {
	ctx         context.Context
	fallbackCtx context.Context
	attributes  map[string]attribute.Value
	mu          sync.RWMutex
}

func (m *sentryMeter) emit(ctx context.Context, metricType MetricType, name string, value MetricValue, opts []MeterOption) {
	if name == "" {
		debuglog.Println("empty name provided, dropping metric")
		return
	}

	client := clientFromContexts(ctx, m.fallbackCtx)
	if !client.IsEnabled() {
		return
	}
	var options meterOptions
	for _, option := range opts {
		option(&options)
	}
	fallbackCtx := m.fallbackCtx
	scope := ScopeFromContext(ctx)
	if scope == nil {
		scope = ScopeFromContext(fallbackCtx)
	} else {
		fallbackCtx = nil
	}
	if options.scope != nil {
		scope = options.scope
	}

	m.mu.RLock()
	attrs, propagation := mergeScopeAttributes(client, scope, len(m.attributes)+len(options.attributes))
	for k, v := range m.attributes {
		attrs[k] = v
	}
	m.mu.RUnlock()

	for k, v := range options.attributes {
		attrs[k] = v
	}

	metric := &Metric{
		Timestamp:  time.Now(),
		Type:       metricType,
		Name:       name,
		Value:      value,
		Unit:       options.unit,
		Attributes: attrs,
	}
	trace := activeTraceFromContexts(client, ctx, fallbackCtx)
	if trace.traceID != zeroTraceID {
		metric.TraceID, metric.SpanID = trace.traceID, trace.spanID
	} else {
		metric.TraceID = propagation.TraceID
	}

	if client.captureMetric(metric) && client.options.Debug {
		debuglog.Printf("Metric %s [%s]: %v %s", metricType, name, value.AsInterface(), options.unit)
	}
}

// WithCtx returns a new Meter that uses the given context for trace/span association.
func (m *sentryMeter) WithCtx(ctx context.Context) Meter {
	m.mu.RLock()
	attrsCopy := maps.Clone(m.attributes)
	m.mu.RUnlock()
	fallbackCtx := m.fallbackCtx
	if fallbackCtx == nil {
		fallbackCtx = m.ctx
	}

	return &sentryMeter{
		ctx:         ctx,
		fallbackCtx: fallbackCtx,
		attributes:  attrsCopy,
	}
}

// Count implements Meter.
func (m *sentryMeter) Count(name string, count int64, opts ...MeterOption) {
	m.emit(m.ctx, MetricTypeCounter, name, Int64MetricValue(count), opts)
}

// Distribution implements Meter.
func (m *sentryMeter) Distribution(name string, sample float64, opts ...MeterOption) {
	m.emit(m.ctx, MetricTypeDistribution, name, Float64MetricValue(sample), opts)
}

// Gauge implements Meter.
func (m *sentryMeter) Gauge(name string, value float64, opts ...MeterOption) {
	m.emit(m.ctx, MetricTypeGauge, name, Float64MetricValue(value), opts)
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
		if m.attributes == nil {
			m.attributes = make(map[string]attribute.Value, len(attrs))
		}
		m.attributes[a.Key] = a.Value
	}
}
