package sentry

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/getsentry/sentry-go/attribute"
	"github.com/getsentry/sentry-go/internal/debuglog"
)

func NewMeter(ctx context.Context) Meter {
	var hub *Hub
	hub = GetHubFromContext(ctx)
	if hub == nil {
		hub = CurrentHub()
	}

	client := hub.Client()
	if client != nil && client.batchMeter != nil {
		return &sentryMeter{
			ctx:        ctx,
			client:     client,
			attributes: make(map[string]Attribute),
			mu:         sync.RWMutex{},
		}
	}

	return &noopMeter{}
}

type sentryMeter struct {
	ctx        context.Context
	client     *Client
	attributes map[string]Attribute
	mu         sync.RWMutex
}

var _ Meter = (*sentryMeter)(nil)

func (s *sentryMeter) emit(ctx context.Context, metricType MetricType, name string, value float64, unit string, attributes map[string]Attribute) {
	if name == "" {
		return
	}

	hub := GetHubFromContext(ctx)
	if hub == nil {
		hub = CurrentHub()
	}

	var traceID TraceID
	var spanID SpanID
	var span *Span
	var user User

	scope := hub.Scope()
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

	attrs := map[string]Attribute{}
	s.mu.RLock()
	for k, v := range s.attributes {
		attrs[k] = v
	}
	s.mu.RUnlock()

	for k, v := range attributes {
		attrs[k] = v
	}

	// Set default attributes
	if release := s.client.options.Release; release != "" {
		attrs["sentry.release"] = Attribute{Value: release, Type: AttributeString}
	}
	if environment := s.client.options.Environment; environment != "" {
		attrs["sentry.environment"] = Attribute{Value: environment, Type: AttributeString}
	}
	if serverName := s.client.options.ServerName; serverName != "" {
		attrs["sentry.server.address"] = Attribute{Value: serverName, Type: AttributeString}
	} else if serverAddr, err := os.Hostname(); err == nil {
		attrs["sentry.server.address"] = Attribute{Value: serverAddr, Type: AttributeString}
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
	if span != nil {
		attrs["sentry.trace.parent_span_id"] = Attribute{Value: spanID.String(), Type: AttributeString}
	}
	if sdkIdentifier := s.client.sdkIdentifier; sdkIdentifier != "" {
		attrs["sentry.sdk.name"] = Attribute{Value: sdkIdentifier, Type: AttributeString}
	}
	if sdkVersion := s.client.sdkVersion; sdkVersion != "" {
		attrs["sentry.sdk.version"] = Attribute{Value: sdkVersion, Type: AttributeString}
	}

	metric := &Metric{
		Timestamp:  time.Now(),
		TraceID:    traceID,
		Type:       metricType,
		Name:       name,
		Value:      value,
		Unit:       unit,
		Attributes: attributes,
	}
	s.client.batchMeter.metricsCh <- *metric

	if s.client.options.Debug {
		debuglog.Printf("Metric %s [%s]: %f %s", metricType, name, value, unit)
	}
}

// Count implements Meter.
func (s *sentryMeter) Count(name string, count int64, options MeterOptions) {
	// count can be negative, but if it's 0, then don't send anything
	if count == 0 {
		return
	}

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

	s.emit(s.ctx, MetricTypeCounter, name, float64(count), "", attrs)
}

// Distribution implements Meter.
func (s *sentryMeter) Distribution(name string, sample float64, options MeterOptions) {
	if sample == 0 {
		return
	}

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

	s.emit(s.ctx, MetricTypeDistribution, name, sample, "", attrs)
}

// FGauge implements Meter.
func (s *sentryMeter) FGauge(name string, value float64, options MeterOptions) {
	if value == 0 {
		return
	}

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

	s.emit(s.ctx, MetricTypeGauge, name, value, "", attrs)
}

// Gauge implements Meter.
func (s *sentryMeter) Gauge(name string, value int64, options MeterOptions) {
	if value == 0 {
		return
	}

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

	s.emit(s.ctx, MetricTypeGauge, name, float64(value), "", attrs)
}

// GetCtx implements Meter.
func (s *sentryMeter) GetCtx() context.Context {
	return s.ctx
}

// SetAttributes implements Meter.
func (s *sentryMeter) SetAttributes(attrs ...attribute.Builder) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, v := range attrs {
		t, ok := mapTypesToStr[v.Value.Type()]
		if !ok || t == "" {
			debuglog.Printf("invalid attribute type set: %v", t)
			continue
		}

		s.attributes[v.Key] = Attribute{
			Value: v.Value.AsInterface(),
			Type:  t,
		}
	}
}

type noopMeter struct{}

var _ Meter = (*noopMeter)(nil)

// Count implements Meter.
func (n *noopMeter) Count(name string, count int64, options MeterOptions) {
}

// Distribution implements Meter.
func (n *noopMeter) Distribution(name string, sample float64, options MeterOptions) {
}

// FGauge implements Meter.
func (n *noopMeter) FGauge(name string, value float64, options MeterOptions) {
}

// Gauge implements Meter.
func (n *noopMeter) Gauge(name string, value int64, options MeterOptions) {
}

// GetCtx implements Meter.
func (n *noopMeter) GetCtx() context.Context {
	return context.Background()
}

// SetAttributes implements Meter.
func (n *noopMeter) SetAttributes(...attribute.Builder) {
}
