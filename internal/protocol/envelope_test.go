package protocol

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestEnvelope_Serialization(t *testing.T) {
	sentAt := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	header := &EnvelopeHeader{
		EventID: "9ec79c33ec9942ab8353589fcb2e04dc",
		SentAt:  sentAt,
		Dsn:     "https://e12d836b15bb49d7bbf99e64295d995b@sentry.io/42",
		Sdk: map[string]interface{}{
			"name":    "sentry.go",
			"version": "1.0.0",
		},
	}

	envelope := NewEnvelope(header)
	eventPayload := []byte(`{"message":"hello world","level":"error"}`)
	eventItem := NewEnvelopeItem(EnvelopeItemTypeEvent, eventPayload)
	envelope.AddItem(eventItem)

	attachmentPayload := []byte("\xef\xbb\xbfHello\r\n")
	attachmentItem := NewAttachmentItem("hello.txt", "text/plain", attachmentPayload)
	envelope.AddItem(attachmentItem)

	data, err := envelope.Serialize()
	if err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}

	lines := strings.Split(string(data), "\n")

	if len(lines) < 5 {
		t.Errorf("Expected at least 5 lines, got %d", len(lines))
	}

	var envelopeHeader map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &envelopeHeader); err != nil {
		t.Errorf("Failed to parse envelope header: %v", err)
	}
	if envelopeHeader["event_id"] != header.EventID {
		t.Errorf("Expected event_id %s, got %v", header.EventID, envelopeHeader["event_id"])
	}

	if strings.Count(lines[0], "\n") > 0 {
		t.Error("Envelope header should be single line")
	}
	if strings.Count(lines[1], "\n") > 0 {
		t.Error("Item header should be single line")
	}

	if strings.Contains(string(data[:len(data)-len(attachmentPayload)]), "\r\n") {
		t.Error("Envelope format should use UNIX newlines \\n only")
	}

	if lines[2] != string(eventPayload) {
		t.Errorf("Event payload mismatch: got %q, want %q", lines[2], string(eventPayload))
	}

	if !strings.Contains(string(data), "\xef\xbb\xbfHello\r\n") {
		t.Error("Attachment payload with Windows newline not preserved")
	}

	sentAtStr, ok := envelopeHeader["sent_at"].(string)
	if !ok {
		t.Errorf("sent_at field is not a string")
	} else {
		rfc3339Regex := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z?$`)
		if !rfc3339Regex.MatchString(sentAtStr) {
			t.Errorf("sent_at timestamp %q is not in RFC 3339 format", sentAtStr)
		}
	}

	uuidTests := []string{
		"12c2d058d58442709aa2eca08bf20986",
		"12c2d058-d584-4270-9aa2-eca08bf20986",
		"12C2D058D58442709AA2ECA08BF20986",
	}
	for _, uuid := range uuidTests {
		testHeader := &EnvelopeHeader{EventID: uuid}
		testEnvelope := NewEnvelope(testHeader)
		testData, err := testEnvelope.Serialize()
		if err != nil {
			t.Errorf("Failed to serialize envelope with UUID %s: %v", uuid, err)
		}
		if !strings.Contains(string(testData), uuid) {
			t.Errorf("UUID %s not preserved in serialization", uuid)
		}
	}

	emptyEnvelope := NewEnvelope(&EnvelopeHeader{EventID: "test"})
	emptyData, err := emptyEnvelope.Serialize()
	if err != nil {
		t.Errorf("Failed to serialize empty envelope: %v", err)
	}
	emptyLines := strings.Split(string(emptyData), "\n")
	if len(emptyLines) < 2 {
		t.Errorf("Empty envelope should have at least 2 lines, got %d", len(emptyLines))
	}

	integrationHeader := &EnvelopeHeader{
		EventID: "12345678901234567890123456789012",
		SentAt:  sentAt,
		Dsn:     "https://public@example.com/1",
		Trace:   map[string]string{"trace_id": "abc123", "public_key": "public"},
	}
	integrationEnvelope := NewEnvelope(integrationHeader)

	integrationEnvelope.AddItem(NewEnvelopeItem(EnvelopeItemTypeEvent, []byte(`{"message": "test event"}`)))
	integrationEnvelope.AddItem(NewAttachmentItem("screenshot.png", "image/png", []byte("fake png data")))
	integrationEnvelope.AddItem(NewLogItem(2, []byte(`[{"message": "log1"}, {"message": "log2"}]`)))

	integrationData, err := integrationEnvelope.Serialize()
	if err != nil {
		t.Errorf("Failed to serialize multi-item envelope: %v", err)
	}

	integrationLines := strings.Split(string(integrationData), "\n")
	if len(integrationLines) < 7 {
		t.Errorf("Expected at least 7 lines for multi-item envelope, got %d", len(integrationLines))
	}

	lineIndex := 1
	for i := 0; i < len(integrationEnvelope.Items); i++ {
		var itemHeader map[string]interface{}
		if err := json.Unmarshal([]byte(integrationLines[lineIndex]), &itemHeader); err != nil {
			t.Errorf("Failed to parse item header %d: %v", i, err)
		}
		if itemHeader["type"] == nil {
			t.Errorf("Item %d missing required type field", i)
		}
		lineIndex++

		if lineIndex < len(integrationLines) {
			payload := integrationLines[lineIndex]
			if len(payload) == 0 && len(integrationEnvelope.Items[i].Payload) > 0 {
				t.Errorf("Expected non-empty payload for item %d", i)
			}
		}
		lineIndex++
	}
}

func TestEnvelope_ItemsAndTypes(t *testing.T) {
	envelope := NewEnvelope(&EnvelopeHeader{EventID: "test-items"})

	itemTests := []struct {
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
			name:     "session",
			itemType: EnvelopeItemTypeSession,
			payload:  []byte(`{"started":"2020-02-07T14:16:00Z","attrs":{"release":"test@1.0.0"}}`),
			creator:  func(p []byte) *EnvelopeItem { return NewEnvelopeItem(EnvelopeItemTypeSession, p) },
		},
		{
			name:     "log",
			itemType: EnvelopeItemTypeLog,
			payload:  []byte(`[{"timestamp":"2023-01-01T12:00:00Z","level":"info","message":"test log"}]`),
			creator:  func(p []byte) *EnvelopeItem { return NewLogItem(1, p) },
		},
	}

	for _, tt := range itemTests {
		t.Run(tt.name, func(t *testing.T) {
			testEnvelope := NewEnvelope(&EnvelopeHeader{EventID: "test"})
			item := tt.creator(tt.payload)
			testEnvelope.AddItem(item)

			if len(testEnvelope.Items) != 1 {
				t.Errorf("Expected 1 item, got %d", len(testEnvelope.Items))
			}

			data, err := testEnvelope.Serialize()
			if err != nil {
				t.Fatalf("Serialize() error = %v", err)
			}

			lines := strings.Split(string(data), "\n")
			if len(lines) < 3 {
				t.Errorf("Expected at least 3 lines, got %d", len(lines))
			}

			var itemHeader map[string]interface{}
			if err := json.Unmarshal([]byte(lines[1]), &itemHeader); err != nil {
				t.Errorf("Failed to parse item header: %v", err)
			}

			if itemHeader["type"] != string(tt.itemType) {
				t.Errorf("Expected type %s, got %v", tt.itemType, itemHeader["type"])
			}

			requiresLength := tt.itemType == EnvelopeItemTypeEvent ||
				tt.itemType == EnvelopeItemTypeTransaction ||
				tt.itemType == EnvelopeItemTypeAttachment ||
				tt.itemType == EnvelopeItemTypeCheckIn ||
				tt.itemType == EnvelopeItemTypeLog

			if requiresLength && itemHeader["length"] == nil {
				t.Errorf("Expected length field for %s item type", tt.itemType)
			}

			if lines[2] != string(tt.payload) {
				t.Errorf("Payload mismatch for %s: got %q, want %q", tt.name, lines[2], string(tt.payload))
			}
		})
	}

	eventItem := NewEnvelopeItem(EnvelopeItemTypeEvent, []byte(`{"test":"event"}`))
	attachmentItem := NewAttachmentItem("file.txt", "text/plain", []byte("content"))

	envelope.AddItem(eventItem)
	envelope.AddItem(attachmentItem)

	if len(envelope.Items) != 2 {
		t.Errorf("Expected 2 items after adding, got %d", len(envelope.Items))
	}

	data, err := envelope.Serialize()
	if err != nil {
		t.Fatalf("Failed to serialize envelope with multiple items: %v", err)
	}

	lines := strings.Split(string(data), "\n")
	if len(lines) < 5 {
		t.Errorf("Expected at least 5 lines for multi-item envelope, got %d", len(lines))
	}

	var eventHeader, attachmentHeader map[string]interface{}
	json.Unmarshal([]byte(lines[1]), &eventHeader)
	json.Unmarshal([]byte(lines[3]), &attachmentHeader)

	if eventHeader["type"] != "event" {
		t.Errorf("First item should be event type, got %v", eventHeader["type"])
	}
	if attachmentHeader["type"] != "attachment" {
		t.Errorf("Second item should be attachment type, got %v", attachmentHeader["type"])
	}
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

func TestEnvelopeItemHeader_MarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		header   *EnvelopeItemHeader
		expected map[string]interface{}
	}{
		{
			name: "log item",
			header: &EnvelopeItemHeader{
				Type:        EnvelopeItemTypeLog,
				ItemCount:   &[]int{5}[0],
				ContentType: "application/vnd.sentry.items.log+json",
			},
			expected: map[string]interface{}{
				"type":         "log",
				"item_count":   float64(5), // JSON numbers are float64
				"content_type": "application/vnd.sentry.items.log+json",
			},
		},
		{
			name: "attachment item",
			header: &EnvelopeItemHeader{
				Type:        EnvelopeItemTypeAttachment,
				Length:      &[]int{100}[0],
				Filename:    "test.txt",
				ContentType: "text/plain",
			},
			expected: map[string]interface{}{
				"type":         "attachment",
				"length":       float64(100),
				"filename":     "test.txt",
				"content_type": "text/plain",
			},
		},
		{
			name: "event item",
			header: &EnvelopeItemHeader{
				Type:   EnvelopeItemTypeEvent,
				Length: &[]int{200}[0],
			},
			expected: map[string]interface{}{
				"type":   "event",
				"length": float64(200),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := tt.header.MarshalJSON()
			if err != nil {
				t.Errorf("MarshalJSON() error = %v", err)
				return
			}

			var result map[string]interface{}
			if err := json.Unmarshal(data, &result); err != nil {
				t.Errorf("Marshaled JSON is invalid: %v", err)
				return
			}

			if diff := cmp.Diff(tt.expected, result); diff != "" {
				t.Errorf("MarshalJSON() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
