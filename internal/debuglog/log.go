package debuglog

import (
	"io"
	"log"
	"sync"
)

var (
	logger = log.New(io.Discard, "[Sentry] ", log.LstdFlags)
	mu     sync.RWMutex
)

// SetLogger replaces the current debug logger with a new one.
// This function is thread-safe and can be called concurrently.
func SetLogger(l *log.Logger) {
	mu.Lock()
	defer mu.Unlock()
	logger = l
}

func SetOutput(w io.Writer) {
	logger.SetOutput(w)
}

// GetLogger returns the current logger instance.
// This function is thread-safe and can be called concurrently.
func GetLogger() *log.Logger {
	mu.RLock()
	defer mu.RUnlock()
	return logger
}

// Printf calls Printf on the underlying logger.
// This function is thread-safe and can be called concurrently.
func Printf(format string, args ...interface{}) {
	mu.RLock()
	l := logger
	mu.RUnlock()
	if l != nil {
		l.Printf(format, args...)
	}
}

// Println calls Println on the underlying logger.
// This function is thread-safe and can be called concurrently.
func Println(args ...interface{}) {
	mu.RLock()
	l := logger
	mu.RUnlock()
	if l != nil {
		l.Println(args...)
	}
}

// Print calls Print on the underlying logger.
// This function is thread-safe and can be called concurrently.
func Print(args ...interface{}) {
	mu.RLock()
	l := logger
	mu.RUnlock()
	if l != nil {
		l.Print(args...)
	}
}
