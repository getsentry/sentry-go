package httputils

import (
	"bytes"
	"io"
	"sync"
)

// MaxBodyBytes is the maximum number of bytes the SDK captures from an HTTP
// body for attaching to events or spans. Bodies larger than this are dropped
// rather than truncated, to avoid invalid partial structured payloads.
const MaxBodyBytes = 10 * 1024

// ReadCloser combines an io.Reader and an io.Closer to implement io.ReadCloser.
type ReadCloser struct {
	io.Reader
	io.Closer
}

// LimitedBuffer is like a bytes.Buffer, but limited to store at most Capacity
// bytes. Any writes past the capacity are silently discarded, similar to
// io.Discard.
//
// A LimitedBuffer is safe for concurrent use. It is typically filled from the
// goroutine that reads the request body, while the buffered bytes are read
// from whichever goroutine captures an event.
type LimitedBuffer struct {
	Capacity int

	mu       sync.Mutex
	buf      bytes.Buffer
	overflow bool
}

// NewLimitedBuffer returns a LimitedBuffer with the given capacity.
func NewLimitedBuffer(capacity int) *LimitedBuffer {
	return &LimitedBuffer{Capacity: capacity}
}

// NewLimitedBufferFromBytes returns a LimitedBuffer initialized from b.
func NewLimitedBufferFromBytes(capacity int, b []byte) *LimitedBuffer {
	buf := NewLimitedBuffer(capacity)
	if len(b) > capacity {
		buf.overflow = true
		b = b[:capacity]
	}
	buf.buf = *bytes.NewBuffer(b)
	return buf
}

// Write implements io.Writer.
func (b *LimitedBuffer) Write(p []byte) (n int, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	originalLen := len(p)
	if b.overflow {
		return originalLen, nil
	}
	left := b.Capacity - b.buf.Len()
	if left < 0 {
		left = 0
	}
	if len(p) > left {
		b.overflow = true
		p = p[:left]
	}
	_, err = b.buf.Write(p)
	return originalLen, err
}

// Bytes returns a copy of the buffered bytes. The copy keeps the result stable
// while the buffer keeps growing.
func (b *LimitedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.buf.Len() == 0 {
		return nil
	}
	return bytes.Clone(b.buf.Bytes())
}

// Len returns the number of buffered bytes.
func (b *LimitedBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Len()
}

// String returns the buffered bytes as a string.
func (b *LimitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// Overflow returns true if the LimitedBuffer discarded bytes written to it.
func (b *LimitedBuffer) Overflow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.overflow
}

// ReadBody reads up to MaxBodyBytes from r and returns the bytes read.
func ReadBody(r io.Reader) []byte {
	if r == nil {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(r, MaxBodyBytes+1))
	if err != nil || len(data) == 0 || len(data) > MaxBodyBytes {
		return nil
	}
	return data
}
