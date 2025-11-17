package telemetry

import (
	"context"
	"testing"

	"github.com/getsentry/sentry-go/internal/protocol"
	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/getsentry/sentry-go/internal/testutils"
)

type bwItem struct{ id string }

func (b bwItem) ToEnvelopeItem() (*protocol.EnvelopeItem, error) {
	return &protocol.EnvelopeItem{Header: &protocol.EnvelopeItemHeader{Type: protocol.EnvelopeItemTypeEvent}, Payload: []byte(`{"message":"ok"}`)}, nil
}
func (b bwItem) GetCategory() ratelimit.Category              { return ratelimit.CategoryError }
func (b bwItem) GetEventID() string                           { return b.id }
func (b bwItem) GetSdkInfo() *protocol.SdkInfo                { return &protocol.SdkInfo{Name: "t", Version: "1"} }
func (b bwItem) GetDynamicSamplingContext() map[string]string { return nil }

func TestBuffer_Add_MissingCategory(t *testing.T) {
	transport := &testutils.MockTelemetryTransport{}
	dsn := &protocol.Dsn{}
	sdk := &protocol.SdkInfo{Name: "s", Version: "v"}
	storage := map[ratelimit.Category]Storage[protocol.EnvelopeItemConvertible]{}

	b := NewBuffer(storage, transport, dsn, sdk)
	ok := b.Add(bwItem{id: "1"})
	if ok {
		t.Fatal("expected Add to return false without storage for category")
	}
	b.Close(testutils.FlushTimeout())
}

func TestBuffer_AddAndFlush_Sends(t *testing.T) {
	transport := &testutils.MockTelemetryTransport{}
	dsn := &protocol.Dsn{}
	sdk := &protocol.SdkInfo{Name: "s", Version: "v"}
	storage := map[ratelimit.Category]Storage[protocol.EnvelopeItemConvertible]{
		ratelimit.CategoryError: NewRingBuffer[protocol.EnvelopeItemConvertible](ratelimit.CategoryError, 10, OverflowPolicyDropOldest, 1, 0),
	}
	b := NewBuffer(storage, transport, dsn, sdk)
	if !b.Add(bwItem{id: "1"}) {
		t.Fatal("add failed")
	}
	if ok := b.Flush(testutils.FlushTimeout()); !ok {
		t.Fatal("flush returned false")
	}
	if ok := b.FlushWithContext(context.Background()); !ok {
		t.Fatal("flush returned false")
	}
	b.Close(testutils.FlushTimeout())
	if transport.GetSendCount() == 0 {
		t.Fatal("expected at least one send")
	}
}
