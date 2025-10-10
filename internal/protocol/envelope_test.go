package protocol

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEnvelope_ItemsAndSerialization(t *testing.T) {
	tests := []struct {
		name     string
		itemType EnvelopeItemType
		payload  []byte
		creator  func([]byte) *EnvelopeItem
	}{
		{
			name:     "event",
			itemType: EnvelopeItemTypeEvent,
			payload:  []byte(`{"message":"test event","level":"error"}`),
			creator:  func(p []byte) *EnvelopeItem { return NewEnvelopeItem(EnvelopeItemTypeEvent, p) },
		},
		{
			name:     "transaction",
			itemType: EnvelopeItemTypeTransaction,
			payload:  []byte(`{"transaction":"test-transaction","type":"transaction"}`),
			creator:  func(p []byte) *EnvelopeItem { return NewEnvelopeItem(EnvelopeItemTypeTransaction, p) },
		},
		{
			name:     "check-in",
			itemType: EnvelopeItemTypeCheckIn,
			payload:  []byte(`{"check_in_id":"abc123","monitor_slug":"test","status":"ok"}`),
			creator:  func(p []byte) *EnvelopeItem { return NewEnvelopeItem(EnvelopeItemTypeCheckIn, p) },
		},
		{
			name:     "attachment",
			itemType: EnvelopeItemTypeAttachment,
			payload:  []byte("test attachment content"),
			creator:  func(p []byte) *EnvelopeItem { return NewAttachmentItem("test.txt", "text/plain", p) },
		},
		{
			name:     "log",
			itemType: EnvelopeItemTypeLog,
			payload:  []byte(`[{"timestamp":"2023-01-01T12:00:00Z","level":"info","message":"test log"}]`),
			creator:  func(p []byte) *EnvelopeItem { return NewLogItem(1, p) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header := &EnvelopeHeader{
				EventID: "9ec79c33ec9942ab8353589fcb2e04dc",
				SentAt:  time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
			}
			envelope := NewEnvelope(header)
			item := tt.creator(tt.payload)
			envelope.AddItem(item)

			data, err := envelope.Serialize()
			if err != nil {
				t.Fatalf("Serialize() failed for %s: %v", tt.name, err)
			}

			lines := strings.Split(string(data), "\n")
			if len(lines) < 3 {
				t.Fatalf("Expected at least 3 lines for %s, got %d", tt.name, len(lines))
			}

			var envelopeHeader map[string]interface{}
			if err := json.Unmarshal([]byte(lines[0]), &envelopeHeader); err != nil {
				t.Fatalf("Failed to unmarshal envelope header: %v", err)
			}

			var itemHeader map[string]interface{}
			if err := json.Unmarshal([]byte(lines[1]), &itemHeader); err != nil {
				t.Fatalf("Failed to unmarshal item header: %v", err)
			}

			if itemHeader["type"] != string(tt.itemType) {
				t.Errorf("Expected type %s, got %v", tt.itemType, itemHeader["type"])
			}

			if lines[2] != string(tt.payload) {
				t.Errorf("Payload not preserved for %s", tt.name)
			}
		})
	}

	t.Run("multi-item envelope", func(t *testing.T) {
		header := &EnvelopeHeader{
			EventID: "multi-test",
			SentAt:  time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
		}
		envelope := NewEnvelope(header)

		envelope.AddItem(NewEnvelopeItem(EnvelopeItemTypeEvent, []byte(`{"message":"test"}`)))
		envelope.AddItem(NewAttachmentItem("file.txt", "text/plain", []byte("content")))
		envelope.AddItem(NewLogItem(1, []byte(`[{"level":"info"}]`)))

		data, err := envelope.Serialize()
		if err != nil {
			t.Fatalf("Multi-item serialize failed: %v", err)
		}

		if len(envelope.Items) != 3 {
			t.Errorf("Expected 3 items, got %d", len(envelope.Items))
		}

		if len(data) == 0 {
			t.Error("Serialized data is empty")
		}
	})

	t.Run("empty envelope", func(t *testing.T) {
		envelope := NewEnvelope(&EnvelopeHeader{EventID: "empty-test"})
		data, err := envelope.Serialize()
		if err != nil {
			t.Fatalf("Empty envelope serialize failed: %v", err)
		}
		if len(data) == 0 {
			t.Error("Empty envelope should still produce header data")
		}
	})
}

func TestEnvelope_WriteTo(t *testing.T) {
	header := &EnvelopeHeader{
		EventID: "12345678901234567890123456789012",
	}
	envelope := NewEnvelope(header)
	envelope.AddItem(NewEnvelopeItem(EnvelopeItemTypeEvent, []byte(`{"test": true}`)))

	var buf bytes.Buffer
	n, err := envelope.WriteTo(&buf)

	if err != nil {
		t.Errorf("WriteTo() error = %v", err)
	}

	if n <= 0 {
		t.Errorf("Expected positive bytes written, got %d", n)
	}

	expectedData, _ := envelope.Serialize()
	if !bytes.Equal(buf.Bytes(), expectedData) {
		t.Errorf("WriteTo() data differs from Serialize()")
	}

	if int64(len(expectedData)) != n {
		t.Errorf("WriteTo() returned %d bytes, but wrote %d bytes", n, len(expectedData))
	}
}

func TestEnvelope_Size(t *testing.T) {
	header := &EnvelopeHeader{EventID: "test"}
	envelope := NewEnvelope(header)

	size1, err := envelope.Size()
	if err != nil {
		t.Errorf("Size() error = %v", err)
	}

	envelope.AddItem(NewEnvelopeItem(EnvelopeItemTypeEvent, []byte(`{"test": true}`)))
	size2, err := envelope.Size()
	if err != nil {
		t.Errorf("Size() error = %v", err)
	}

	if size2 <= size1 {
		t.Errorf("Expected size to increase after adding item, got %d -> %d", size1, size2)
	}

	data, _ := envelope.Serialize()
	if size2 != len(data) {
		t.Errorf("Size() = %d, but Serialize() length = %d", size2, len(data))
	}
}

func TestEnvelopeHeader_MarshalJSON(t *testing.T) {
	header := &EnvelopeHeader{
		EventID: "12345678901234567890123456789012",
		SentAt:  time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
		Dsn:     "https://public@example.com/1",
		Trace:   map[string]string{"trace_id": "abc123"},
	}

	data, err := header.MarshalJSON()
	if err != nil {
		t.Errorf("MarshalJSON() error = %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Errorf("Marshaled JSON is invalid: %v", err)
	}

	if result["event_id"] != header.EventID {
		t.Errorf("Expected event_id %s, got %v", header.EventID, result["event_id"])
	}

	if result["dsn"] != header.Dsn {
		t.Errorf("Expected dsn %s, got %v", header.Dsn, result["dsn"])
	}

	if bytes.Contains(data, []byte("\n")) {
		t.Error("Marshaled JSON contains newlines")
	}
}
