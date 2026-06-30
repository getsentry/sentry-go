package httputils

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

func TestReadBody(t *testing.T) {
	tests := []struct {
		name   string
		reader io.Reader
		want   []byte
	}{
		{name: "nil reader", reader: nil, want: nil},
		{name: "empty reader", reader: strings.NewReader(""), want: nil},
		{name: "small body", reader: strings.NewReader("hello"), want: []byte("hello")},
		{name: "at limit", reader: bytes.NewReader(bytes.Repeat([]byte("a"), MaxBodyBytes)), want: bytes.Repeat([]byte("a"), MaxBodyBytes)},
		{name: "oversized body dropped", reader: bytes.NewReader(bytes.Repeat([]byte("a"), MaxBodyBytes+1)), want: nil},
		{name: "read error dropped", reader: iotest.ErrReader(errors.New("boom")), want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ReadBody(tt.reader)
			if !bytes.Equal(got, tt.want) {
				t.Errorf("ReadBody len=%d, want len=%d", len(got), len(tt.want))
			}
		})
	}
}

func TestLimitedBuffer(t *testing.T) {
	t.Run("stores bytes within capacity", func(t *testing.T) {
		buf := NewLimitedBuffer(5)
		n, err := buf.Write([]byte("hello"))
		if err != nil {
			t.Fatal(err)
		}
		if n != 5 {
			t.Fatalf("Write returned %d, want 5", n)
		}
		if got := string(buf.Bytes()); got != "hello" {
			t.Fatalf("Bytes = %q, want %q", got, "hello")
		}
		if buf.Overflow() {
			t.Fatal("Overflow = true, want false")
		}
	})

	t.Run("drops bytes after capacity", func(t *testing.T) {
		buf := NewLimitedBuffer(5)
		n, err := buf.Write([]byte("hello world"))
		if err != nil {
			t.Fatal(err)
		}
		if n != len("hello world") {
			t.Fatalf("Write returned %d, want %d", n, len("hello world"))
		}
		if got := string(buf.Bytes()); got != "hello" {
			t.Fatalf("Bytes = %q, want %q", got, "hello")
		}
		if !buf.Overflow() {
			t.Fatal("Overflow = false, want true")
		}
	})

	t.Run("initial bytes mark overflow", func(t *testing.T) {
		buf := NewLimitedBufferFromBytes(5, []byte("hello world"))
		if got := string(buf.Bytes()); got != "hello" {
			t.Fatalf("Bytes = %q, want %q", got, "hello")
		}
		if !buf.Overflow() {
			t.Fatal("Overflow = false, want true")
		}
	})
}
