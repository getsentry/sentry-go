package sentry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// Envelope represents a Sentry envelope containing headers and items.
type Envelope struct {
	Header *EnvelopeHeader `json:"-"`
	Items  []*EnvelopeItem `json:"-"`
}

// NewEnvelope creates a new envelope with the given header.
func NewEnvelope(header *EnvelopeHeader) *Envelope {
	return &Envelope{
		Header: header,
		Items:  make([]*EnvelopeItem, 0),
	}
}

// AddItem adds an item to the envelope.
func (e *Envelope) AddItem(item *EnvelopeItem) {
	e.Items = append(e.Items, item)
}

// Serialize serializes the envelope to the Sentry envelope format.
// Format: Headers "\n" { Item } [ "\n" ]
// Item: Headers "\n" Payload "\n"
func (e *Envelope) Serialize() ([]byte, error) {
	var buf bytes.Buffer

	headerBytes, err := json.Marshal(e.Header)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal envelope header: %w", err)
	}

	if _, err := buf.Write(headerBytes); err != nil {
		return nil, fmt.Errorf("failed to write envelope header: %w", err)
	}

	if _, err := buf.WriteString("\n"); err != nil {
		return nil, fmt.Errorf("failed to write newline after envelope header: %w", err)
	}

	for _, item := range e.Items {
		if err := e.writeItem(&buf, item); err != nil {
			return nil, fmt.Errorf("failed to write envelope item: %w", err)
		}
	}

	return buf.Bytes(), nil
}

// WriteTo writes the envelope to the given writer in the Sentry envelope format.
func (e *Envelope) WriteTo(w io.Writer) (int64, error) {
	data, err := e.Serialize()
	if err != nil {
		return 0, err
	}

	n, err := w.Write(data)
	return int64(n), err
}

// writeItem writes a single envelope item to the buffer.
func (e *Envelope) writeItem(buf *bytes.Buffer, item *EnvelopeItem) error {
	headerBytes, err := json.Marshal(item.Header)
	if err != nil {
		return fmt.Errorf("failed to marshal item header: %w", err)
	}

	if _, err := buf.Write(headerBytes); err != nil {
		return fmt.Errorf("failed to write item header: %w", err)
	}

	if _, err := buf.WriteString("\n"); err != nil {
		return fmt.Errorf("failed to write newline after item header: %w", err)
	}

	if len(item.Payload) > 0 {
		if _, err := buf.Write(item.Payload); err != nil {
			return fmt.Errorf("failed to write item payload: %w", err)
		}
	}

	if _, err := buf.WriteString("\n"); err != nil {
		return fmt.Errorf("failed to write newline after item payload: %w", err)
	}

	return nil
}

// Size returns the total size of the envelope when serialized.
func (e *Envelope) Size() (int, error) {
	data, err := e.Serialize()
	if err != nil {
		return 0, err
	}
	return len(data), nil
}
