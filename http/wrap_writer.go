package sentryhttp

import (
	"bufio"
	"io"
	"net"
	"net/http"
)

// This wrapper is derived from https://github.com/go-chi/chi/blob/master/middleware/wrap_writer.go
// Copyright (c) 2015-present Peter Kieltyka (https://github.com/pkieltyka), Google Inc.

// MIT License

// Permission is hereby granted, free of charge, to any person obtaining a copy of
// this software and associated documentation files (the "Software"), to deal in
// the Software without restriction, including without limitation the rights to
// use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of
// the Software, and to permit persons to whom the Software is furnished to do so,
// subject to the following conditions:

// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.

// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS
// FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR
// COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER
// IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN
// CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

// NewWrapResponseWriter wraps an http.ResponseWriter, returning a proxy that allows you to
// hook into various parts of the response process.

func NewWrapResponseWriter(w http.ResponseWriter, protoMajor int) WrapResponseWriter {
	_, fl := w.(http.Flusher)

	bw := basicWriter{ResponseWriter: w, code: http.StatusOK}

	if protoMajor == 2 {
		_, ps := w.(http.Pusher)
		if fl && ps {
			return &http2FancyWriter{bw}
		}
	} else {
		_, hj := w.(http.Hijacker)
		_, rf := w.(io.ReaderFrom)
		if fl && hj && rf {
			return &httpFancyWriter{bw}
		}
	}
	if fl {
		return &flushWriter{bw}
	}

	return &bw
}

// WrapResponseWriter is a proxy around an http.ResponseWriter that allows you to hook
// into various parts of the response process.
type WrapResponseWriter interface {
	http.ResponseWriter
	// Status returns the HTTP status of the request, or 200 if one has not
	// yet been sent.
	Status() int
	// BytesWritten returns the total number of bytes sent to the client.
	BytesWritten() int
	// Tee causes the response body to be written to the given io.Writer in
	// addition to proxying the writes through. Only one io.Writer can be
	// tee'd to at once: setting a second one will overwrite the first.
	// Writes will be sent to the proxy before being written to this
	// io.Writer. It is illegal for the tee'd writer to be modified
	// concurrently with writes.
	Tee(io.Writer)
	// Unwrap returns the original proxied target.
	Unwrap() http.ResponseWriter
}

// basicWriter wraps a http.ResponseWriter that implements the minimal
// http.ResponseWriter interface.
type basicWriter struct {
	http.ResponseWriter
	tee         io.Writer
	code        int
	bytes       int
	wroteHeader bool
}

func (b *basicWriter) WriteHeader(code int) {
	if !b.wroteHeader {
		b.code = code
		b.wroteHeader = true
	}
	b.ResponseWriter.WriteHeader(code)
}

func (b *basicWriter) Write(buf []byte) (int, error) {
	b.maybeWriteHeader()
	n, err := b.ResponseWriter.Write(buf)
	if b.tee != nil {
		_, err2 := b.tee.Write(buf[:n])
		// Prefer errors generated by the proxied writer.
		if err == nil {
			err = err2
		}
	}
	b.bytes += n
	return n, err
}

func (b *basicWriter) maybeWriteHeader() {
	if !b.wroteHeader {
		b.WriteHeader(http.StatusOK)
	}
}

func (b *basicWriter) Status() int {
	return b.code
}

func (b *basicWriter) BytesWritten() int {
	return b.bytes
}

func (b *basicWriter) Tee(w io.Writer) {
	b.tee = w
}

func (b *basicWriter) Unwrap() http.ResponseWriter {
	return b.ResponseWriter
}

type flushWriter struct {
	basicWriter
}

func (f *flushWriter) Flush() {
	f.wroteHeader = true

	fl := f.basicWriter.ResponseWriter.(http.Flusher)
	fl.Flush()
}

var _ http.Flusher = &flushWriter{}

// httpFancyWriter is a HTTP writer that additionally satisfies
// http.Flusher, http.Hijacker, and io.ReaderFrom. It exists for the common case
// of wrapping the http.ResponseWriter that package http gives you, in order to
// make the proxied object support the full method set of the proxied object.
type httpFancyWriter struct {
	basicWriter
}

func (f *httpFancyWriter) Flush() {
	f.wroteHeader = true
	f.basicWriter.ResponseWriter.(http.Flusher).Flush()
}

func (f *httpFancyWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return f.basicWriter.ResponseWriter.(http.Hijacker).Hijack()
}

func (f *http2FancyWriter) Push(target string, opts *http.PushOptions) error {
	return f.basicWriter.ResponseWriter.(http.Pusher).Push(target, opts)
}

func (f *httpFancyWriter) ReadFrom(r io.Reader) (int64, error) {
	if f.basicWriter.tee != nil {
		n, err := io.Copy(&f.basicWriter, r)
		f.basicWriter.bytes += int(n)
		return n, err
	}
	rf := f.basicWriter.ResponseWriter.(io.ReaderFrom)
	f.basicWriter.maybeWriteHeader()
	n, err := rf.ReadFrom(r)
	f.basicWriter.bytes += int(n)
	return n, err
}

var _ http.Flusher = &httpFancyWriter{}
var _ http.Hijacker = &httpFancyWriter{}
var _ http.Pusher = &http2FancyWriter{}
var _ io.ReaderFrom = &httpFancyWriter{}

// http2FancyWriter is a HTTP2 writer that additionally satisfies
// http.Flusher, and io.ReaderFrom. It exists for the common case
// of wrapping the http.ResponseWriter that package http gives you, in order to
// make the proxied object support the full method set of the proxied object.
type http2FancyWriter struct {
	basicWriter
}

func (f *http2FancyWriter) Flush() {
	f.wroteHeader = true

	f.basicWriter.ResponseWriter.(http.Flusher).Flush()
}

var _ http.Flusher = &http2FancyWriter{}
