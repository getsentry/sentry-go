package sentry

import (
	"context"
	"errors"
	"os"

	"github.com/getsentry/sentry-go/attribute"
)

// fallback no-op logger if Sentry is not enabled.
type noopLogger struct{}

func (*noopLogger) Trace(_ context.Context, _ ...interface{}) {
	DebugLogger.Println("does nothing: EnableLogs disabled")
}
func (*noopLogger) Debug(_ context.Context, _ ...interface{}) {
	DebugLogger.Println("does nothing: EnableLogs disabled")
}
func (*noopLogger) Info(_ context.Context, _ ...interface{}) {
	DebugLogger.Println("does nothing: EnableLogs disabled")
}
func (*noopLogger) Warn(_ context.Context, _ ...interface{}) {
	DebugLogger.Println("does nothing: EnableLogs disabled")
}
func (*noopLogger) Error(_ context.Context, _ ...interface{}) {
	DebugLogger.Println("does nothing: EnableLogs disabled")
}
func (*noopLogger) Fatal(_ context.Context, _ ...interface{}) {
	DebugLogger.Println("does nothing: EnableLogs disabled")
	os.Exit(1)
}
func (*noopLogger) Panic(_ context.Context, _ ...interface{}) {
	DebugLogger.Println("does nothing: EnableLogs disabled")
	panic("invalid setup: EnableLogs disabled")
}
func (*noopLogger) Tracef(_ context.Context, _ string, _ ...interface{}) {
	DebugLogger.Println("does nothing: EnableLogs disabled")
}
func (*noopLogger) Debugf(_ context.Context, _ string, _ ...interface{}) {
	DebugLogger.Println("does nothing: EnableLogs disabled")
}
func (*noopLogger) Infof(_ context.Context, _ string, _ ...interface{}) {
	DebugLogger.Println("does nothing: EnableLogs disabled")
}
func (*noopLogger) Warnf(_ context.Context, _ string, _ ...interface{}) {
	DebugLogger.Println("does nothing: EnableLogs disabled")
}
func (*noopLogger) Errorf(_ context.Context, _ string, _ ...interface{}) {
	DebugLogger.Println("does nothing: EnableLogs disabled")
}
func (*noopLogger) Fatalf(_ context.Context, _ string, _ ...interface{}) {
	DebugLogger.Println("does nothing: EnableLogs disabled")
	os.Exit(1)
}
func (*noopLogger) Panicf(_ context.Context, _ string, _ ...interface{}) {
	DebugLogger.Println("does nothing: EnableLogs disabled")
	panic("invalid setup: EnableLogs disabled")
}
func (*noopLogger) SetAttributes(...attribute.Builder) {
	DebugLogger.Println("does nothing: EnableLogs disabled")
}
func (*noopLogger) Write(_ []byte) (n int, err error) {
	return 0, errors.New("does nothing: EnableLogs disabled")
}
