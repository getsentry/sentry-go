package protocol

import (
	"encoding/json"
	"testing"

	"github.com/getsentry/sentry-go/internal/ratelimit"
)

type dummyLog struct{ body string }

func (d dummyLog) ToEnvelopeItem() (*EnvelopeItem, error) {
	payload := []byte(`{"body":"` + d.body + `"}`)
	return &EnvelopeItem{Header: &EnvelopeItemHeader{Type: EnvelopeItemTypeLog}, Payload: payload}, nil
}
func (dummyLog) GetCategory() ratelimit.Category              { return ratelimit.CategoryLog }
func (dummyLog) GetEventID() string                           { return "" }
func (dummyLog) GetSdkInfo() *SdkInfo                         { return nil }
func (dummyLog) GetDynamicSamplingContext() map[string]string { return nil }

func TestLogs_ToEnvelopeItem_And_Getters(t *testing.T) {
	logs := Logs{dummyLog{body: "a"}, dummyLog{body: "b"}}
	item, err := logs.ToEnvelopeItem()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if item == nil || item.Header == nil || item.Header.Type != EnvelopeItemTypeLog {
		t.Fatalf("unexpected envelope item: %#v", item)
	}
	var payload struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(item.Payload, &payload); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(payload.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(payload.Items))
	}

	if Logs(nil).GetCategory() != ratelimit.CategoryLog {
		t.Fatal("category mismatch")
	}
	if Logs(nil).GetEventID() != "" {
		t.Fatal("event id should be empty")
	}
	if Logs(nil).GetSdkInfo() != nil {
		t.Fatal("sdk info should be nil")
	}
	if Logs(nil).GetDynamicSamplingContext() != nil {
		t.Fatal("dsc should be nil")
	}
}
