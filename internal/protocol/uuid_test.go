package protocol

import (
	"encoding/hex"
	"testing"
)

func TestGenerateEventID_FormatVersionVariant(t *testing.T) {
	const n = 100
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		id := GenerateEventID()
		if len(id) != 32 {
			t.Fatalf("length = %d, want 32", len(id))
		}
		b, err := hex.DecodeString(id)
		if err != nil {
			t.Fatalf("id not hex: %v", err)
		}
		if len(b) != 16 {
			t.Fatalf("decoded length = %d, want 16", len(b))
		}
		if v := b[6] & 0xF0; v != 0x40 {
			t.Fatalf("version nibble = 0x%x, want 0x40", v)
		}
		if v := b[8] & 0xC0; v != 0x80 {
			t.Fatalf("variant bits = 0x%x, want 0x80", v)
		}
		if _, exists := seen[id]; exists {
			t.Fatalf("duplicate id generated: %s", id)
		}
		seen[id] = struct{}{}
	}
}
