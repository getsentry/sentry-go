package sentry

import (
	"encoding/json"
)

// EnvelopeItemType represents the type of envelope item.
type EnvelopeItemType string

// Constants for envelope item types as defined in the Sentry documentation.
const (
	EnvelopeItemTypeEvent       EnvelopeItemType = "event"
	EnvelopeItemTypeTransaction EnvelopeItemType = "transaction"
	EnvelopeItemTypeCheckIn     EnvelopeItemType = "check_in"
	EnvelopeItemTypeAttachment  EnvelopeItemType = "attachment"
	EnvelopeItemTypeSession     EnvelopeItemType = "session"
	EnvelopeItemTypeLog         EnvelopeItemType = "log"
	EnvelopeItemTypeProfile     EnvelopeItemType = "profile"
	EnvelopeItemTypeReplay      EnvelopeItemType = "replay"
	EnvelopeItemTypeSpan        EnvelopeItemType = "span"
	EnvelopeItemTypeStatsd      EnvelopeItemType = "statsd"
	EnvelopeItemTypeMetrics     EnvelopeItemType = "metrics"
)

// EnvelopeItemHeader represents the header of an envelope item.
type EnvelopeItemHeader struct {
	// Type specifies the type of this Item and its contents.
	// Based on the Item type, more headers may be required.
	Type EnvelopeItemType `json:"type"`

	// Length is the length of the payload in bytes.
	// If no length is specified, the payload implicitly goes to the next newline.
	// For payloads containing newline characters, the length must be specified.
	Length *int `json:"length,omitempty"`

	// Filename is the name of the attachment file (used for attachments)
	Filename string `json:"filename,omitempty"`

	// ContentType is the MIME type of the item payload (used for attachments and some other item types)
	ContentType string `json:"content_type,omitempty"`

	// ItemCount is the number of items in a batch (used for logs)
	ItemCount *int `json:"item_count,omitempty"`
}

// MarshalJSON provides custom JSON marshaling to handle field ordering for different item types
func (h *EnvelopeItemHeader) MarshalJSON() ([]byte, error) {
	switch h.Type {
	case EnvelopeItemTypeLog:
		// For log items, use the correct field order: type, item_count, content_type
		return json.Marshal(struct {
			Type        EnvelopeItemType `json:"type"`
			ItemCount   *int             `json:"item_count,omitempty"`
			ContentType string           `json:"content_type,omitempty"`
		}{
			Type:        h.Type,
			ItemCount:   h.ItemCount,
			ContentType: h.ContentType,
		})
	case EnvelopeItemTypeAttachment:
		// For attachments, use the correct field order: type, length, filename, content_type
		return json.Marshal(struct {
			Type        EnvelopeItemType `json:"type"`
			Length      *int             `json:"length,omitempty"`
			Filename    string           `json:"filename,omitempty"`
			ContentType string           `json:"content_type,omitempty"`
		}{
			Type:        h.Type,
			Length:      h.Length,
			Filename:    h.Filename,
			ContentType: h.ContentType,
		})
	default:
		// For other item types, use standard field order: type, length
		return json.Marshal(struct {
			Type   EnvelopeItemType `json:"type"`
			Length *int             `json:"length,omitempty"`
		}{
			Type:   h.Type,
			Length: h.Length,
		})
	}
}

// EnvelopeItem represents a single item within an envelope.
type EnvelopeItem struct {
	Header  *EnvelopeItemHeader `json:"-"`
	Payload []byte              `json:"-"`
}

// NewEnvelopeItem creates a new envelope item with the specified type and payload.
func NewEnvelopeItem(itemType EnvelopeItemType, payload []byte) *EnvelopeItem {
	length := len(payload)
	return &EnvelopeItem{
		Header: &EnvelopeItemHeader{
			Type:   itemType,
			Length: &length,
		},
		Payload: payload,
	}
}

// NewAttachmentItem creates a new envelope item for an attachment.
func NewAttachmentItem(attachment *Attachment) *EnvelopeItem {
	length := len(attachment.Payload)
	return &EnvelopeItem{
		Header: &EnvelopeItemHeader{
			Type:        EnvelopeItemTypeAttachment,
			Length:      &length,
			ContentType: attachment.ContentType,
			Filename:    attachment.Filename,
		},
		Payload: attachment.Payload,
	}
}

// NewLogItem creates a new envelope item for logs.
func NewLogItem(logs []Log, payload []byte) *EnvelopeItem {
	length := len(payload)
	itemCount := len(logs)
	return &EnvelopeItem{
		Header: &EnvelopeItemHeader{
			Type:        EnvelopeItemTypeLog,
			Length:      &length,
			ItemCount:   &itemCount,
			ContentType: "application/vnd.sentry.items.log+json",
		},
		Payload: payload,
	}
}
