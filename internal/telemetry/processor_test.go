package telemetry

import (
	"context"
	"testing"

	"github.com/getsentry/sentry-go/internal/ratelimit"
	"github.com/getsentry/sentry-go/internal/testutils"
	"github.com/getsentry/sentry-go/protocol"
)

type bwItem struct{ id string }

func (b bwItem) ToEnvelopeItem() (*protocol.EnvelopeItem, error) {
	return &protocol.EnvelopeItem{
		Header:  &protocol.EnvelopeItemHeader{Type: protocol.EnvelopeItemTypeEvent},
		Payload: []byte(`{"message":"ok"}`),
	}, nil
}
func (b bwItem) ToEnvelope(header *protocol.EnvelopeHeader) (*protocol.Envelope, error) {
	item, err := b.ToEnvelopeItem()
	if err != nil {
		return nil, err
	}
	return protocol.NewEnvelope(header, item), nil
}
func (b bwItem) GetCategory() ratelimit.Category              { return ratelimit.CategoryError }
func (b bwItem) GetEventID() string                           { return b.id }
func (b bwItem) GetSdkInfo() *protocol.SdkInfo                { return &protocol.SdkInfo{Name: "t", Version: "1"} }
func (b bwItem) GetDynamicSamplingContext() map[string]string { return nil }
func (b bwItem) MakeSerializationSafe()                       {}
func (b bwItem) DiscardCounts() DiscardCounts                 { return oneDiscard(ratelimit.CategoryError) }

func TestBuffer_Add_MissingCategory(t *testing.T) {
	var ctxErr error
	transport := &testutils.MockTelemetryTransport{SendFunc: func(ctx context.Context, _ *protocol.Envelope) error {
		ctxErr = ctx.Err()
		return nil
	}}
	dsn := &protocol.Dsn{}
	sdk := &protocol.SdkInfo{Name: "s", Version: "v"}
	storage := map[ratelimit.Category]Buffer[Item]{}

	b := NewProcessor(storage, transport, dsn, func() *protocol.SdkInfo { return sdk }, nil)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if !b.Add(canceled, bwItem{id: "1"}) || transport.GetSendCount() != 1 {
		t.Fatal("expected items without storage to be submitted inline")
	}
	if ctxErr != nil {
		t.Fatalf("inline send inherited capture cancellation: %v", ctxErr)
	}
	b.Close(testutils.FlushTimeout())
	if b.Add(context.Background(), bwItem{id: "2"}) || transport.GetSendCount() != 1 {
		t.Fatal("expected inline items after Close to be rejected")
	}
}
