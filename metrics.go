package sentry

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/getsentry/sentry-go/attribute"
	"github.com/getsentry/sentry-go/internal/debuglog"
)

// NewMeter returns a new Meter associated with the given context.
// If there is no Client associated with the context, or if metrics are disabled,
// it returns a no-op Meter that discards all metrics.
func NewMeter(ctx context.Context) Meter {
	var hub *Hub
	hub = GetHubFromContext(ctx)
	if hub == nil {
		hub = CurrentHub()
	}

	client := hub.Client()
	if client != nil && client.options.EnableMetrics {
		return &sentryMeter{
			hub:        hub,
			client:     client,
			attributes: make(map[string]Attribute),
			mu:         sync.RWMutex{},
		}
	}

	return &noopMeter{}
}

type sentryMeter struct {
	hub        *Hub
	client     *Client
	attributes map[string]Attribute
	mu         sync.RWMutex
}

func (s *sentryMeter) emit(metricType MetricType, name string, value float64, unit string, attributes map[string]Attribute) {
	if name == "" {
		debuglog.Println("empty name provided, dropping metric")
		return
	}

	var traceID TraceID
	var spanID SpanID
	var span *Span
	var user User

	scope := s.hub.Scope()
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
	if sdkIdentifier := s.client.sdkIdentifier; sdkIdentifier != "" {
		attrs["sentry.sdk.name"] = Attribute{Value: sdkIdentifier, Type: AttributeString}
	}
	if sdkVersion := s.client.sdkVersion; sdkVersion != "" {
		attrs["sentry.sdk.version"] = Attribute{Value: sdkVersion, Type: AttributeString}
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

	if s.client.options.BeforeSendMetric != nil {
		metric = s.client.options.BeforeSendMetric(metric)
	}

	if metric != nil {
		if s.client.telemetryBuffer != nil {
			if !s.client.telemetryBuffer.Add(metric) {
				debuglog.Printf("Dropping event: metric buffer full or category missing")
			}
		} else if s.client.batchMeter != nil {
			s.client.batchMeter.Send(metric)
		}
	}

	if s.client.options.Debug {
		debuglog.Printf("Metric %s [%s]: %f %s", metricType, name, value, unit)
	}
}

// Count implements Meter.
func (s *sentryMeter) Count(name string, count int64, options MeterOptions) {
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

	s.emit(MetricTypeCounter, name, float64(count), "", attrs)
}

// Distribution implements Meter.
func (s *sentryMeter) Distribution(name string, sample float64, options MeterOptions) {
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

	s.emit(MetricTypeDistribution, name, sample, options.Unit, attrs)
}

// Gauge implements Meter.
func (s *sentryMeter) Gauge(name string, value float64, options MeterOptions) {
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

	s.emit(MetricTypeGauge, name, value, options.Unit, attrs)
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
