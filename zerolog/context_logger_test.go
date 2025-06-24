package sentryzerolog

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/rs/zerolog"
)

func TestNewContextLogger(t *testing.T) {
	ctx := context.Background()
	opts := Options{
		Levels:       []zerolog.Level{zerolog.InfoLevel, zerolog.ErrorLevel},
		FlushTimeout: time.Second,
	}

	logger := NewLogger(ctx, opts)
	if logger == nil {
		t.Fatal("Expected logger to be non-nil")
	}

	if logger.ctx != ctx {
		t.Error("Expected logger context to match provided context")
	}

	if logger.sentryLogger == nil {
		t.Error("Expected sentry logger to be set")
	}

	if len(logger.levels) != 2 {
		t.Errorf("Expected 2 levels, got %d", len(logger.levels))
	}

	// Test Close method
	err := logger.Close()
	if err != nil {
		t.Errorf("Expected no error on close, got %v", err)
	}
}

func TestNewContextLoggerWithHub(t *testing.T) {
	client, err := sentry.NewClient(sentry.ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	hub := sentry.NewHub(client, sentry.NewScope())
	opts := Options{
		Levels:       []zerolog.Level{zerolog.InfoLevel, zerolog.ErrorLevel},
		FlushTimeout: time.Second,
	}

	ctx := context.Background()
	logger := NewContextLoggerWithHub(hub, ctx, opts)
	if logger == nil {
		t.Fatal("Expected logger to be non-nil")
	}

	if logger.sentryLogger == nil {
		t.Error("Expected sentry logger to be set")
	}

	// Verify that hub is in context
	if sentry.GetHubFromContext(logger.ctx) == nil {
		t.Error("Expected hub to be in logger context")
	}
}

func TestContextLoggerWithContext(t *testing.T) {
	client, err := sentry.NewClient(sentry.ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	hub := sentry.NewHub(client, sentry.NewScope())
	opts := Options{
		Levels:       []zerolog.Level{zerolog.InfoLevel},
		FlushTimeout: time.Second,
	}

	ctx := context.Background()
	logger := NewContextLoggerWithHub(hub, ctx, opts)

	// Create a new context with span
	transaction := sentry.StartTransaction(ctx, "test_transaction")
	defer transaction.Finish()

	newCtx := transaction.Context()
	newLogger := logger.WithContext(newCtx)

	if newLogger.ctx != newCtx {
		t.Error("Expected new logger to have updated context")
	}

	if newLogger.sentryLogger == nil {
		t.Error("Expected sentry logger to be set in new logger")
	}
}

func TestContextLoggerLevels(t *testing.T) {
	ctx := context.Background()
	logger := NewLogger(ctx, Options{
		Levels:       []zerolog.Level{zerolog.InfoLevel, zerolog.ErrorLevel},
		FlushTimeout: time.Second,
	})

	// Test different log levels - should not panic
	logger.Trace().Msg("trace message")
	logger.Debug().Msg("debug message")
	logger.Info().Msg("info message")
	logger.Warn().Msg("warn message")
	logger.Error().Msg("error message")

	// All calls should complete without error
}

func TestContextLoggerSampleAndHook(t *testing.T) {
	ctx := context.Background()
	logger := NewLogger(ctx, Options{
		Levels:       []zerolog.Level{zerolog.InfoLevel},
		FlushTimeout: time.Second,
	})

	// Test Sample method
	sampledLogger := logger.Sample(&zerolog.BasicSampler{N: 1})
	if sampledLogger.ctx != ctx {
		t.Error("Sampled logger should preserve context")
	}
	if sampledLogger.sentryLogger == nil {
		t.Error("Sampled logger should have sentry logger")
	}

	// Test Hook method
	hookCalled := false
	hook := zerolog.HookFunc(func(e *zerolog.Event, level zerolog.Level, message string) {
		hookCalled = true
	})

	hookedLogger := logger.Hook(hook)
	if hookedLogger.ctx != ctx {
		t.Error("Hooked logger should preserve context")
	}
	if hookedLogger.sentryLogger == nil {
		t.Error("Hooked logger should have sentry logger")
	}

	hookedLogger.Info().Msg("test")
	if !hookCalled {
		t.Error("Hook should have been called")
	}
}

func TestContextLoggerMethods(t *testing.T) {
	ctx := context.Background()
	logger := NewLogger(ctx, Options{
		Levels:       []zerolog.Level{zerolog.InfoLevel, zerolog.ErrorLevel},
		FlushTimeout: time.Second,
	})

	// Test various logger methods
	logger.WithLevel(zerolog.InfoLevel).Msg("with level")
	logger.Trace().Msg("trace")
	logger.Debug().Msg("debug")
	logger.Info().Msg("info")
	logger.Warn().Msg("warn")
	logger.Error().Msg("error")
	logger.Fatal().Msg("fatal")
	logger.Panic().Msg("panic")
	logger.Log().Msg("log")
	logger.Err(errors.New("test error")).Msg("error with err")

	// Test With method
	childLogger := logger.With().Str("key", "value").Logger()
	if childLogger.GetLevel() == zerolog.Disabled {
		t.Error("Child logger should not be disabled")
	}

	// Test GetContext
	if logger.GetContext() != ctx {
		t.Error("GetContext should return the logger's context")
	}
}

func TestContextLogWriterLevels(t *testing.T) {
	ctx := context.Background()
	logger := NewLogger(ctx, Options{
		Levels:       []zerolog.Level{zerolog.InfoLevel}, // Only info level enabled
		FlushTimeout: time.Second,
	})

	// Get the underlying writer
	writer := &contextLogWriter{logger: logger}

	// Test enabled level
	n, err := writer.WriteLevel(zerolog.InfoLevel, []byte(`{"level":"info","message":"test"}`))
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if n == 0 {
		t.Error("Expected bytes to be written")
	}

	// Test disabled level - should still return success but not process
	n, err = writer.WriteLevel(zerolog.DebugLevel, []byte(`{"level":"debug","message":"test"}`))
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if n == 0 {
		t.Error("Expected bytes to be returned even for disabled level")
	}
}

func TestContextLogWriterInvalidJSON(t *testing.T) {
	ctx := context.Background()
	logger := NewLogger(ctx, Options{
		Levels:       []zerolog.Level{zerolog.InfoLevel},
		FlushTimeout: time.Second,
	})

	writer := &contextLogWriter{logger: logger}

	// Test invalid JSON - should not fail
	n, err := writer.WriteLevel(zerolog.InfoLevel, []byte(`invalid json`))
	if err != nil {
		t.Errorf("Should not fail on invalid JSON: %v", err)
	}
	if n == 0 {
		t.Error("Should still return bytes written")
	}
}
