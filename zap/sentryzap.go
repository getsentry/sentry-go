package sentryzap

import (
	"errors"
	"fmt"
	"time"

	"github.com/getsentry/sentry-go"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	defaultMaxBreadcrumbs = 100
	maxErrorDepth         = 10
	sentryZapScopeKey     = "_sentryzap_scope_"
)

var (
	ErrInvalidBreadcrumbLevel = errors.New("breadcrumb level must be lower than or equal to error level")
)

type ClientGetter interface {
	GetClient() *sentry.Client
}

// SentryClient is an interface that represents a Sentry client.
type SentryClient interface {
	CaptureEvent(event *sentry.Event, hint *sentry.EventHint, scope *sentry.Scope) string
	Flush(timeout time.Duration) bool
}

// NewSentryClientFromDSN creates a new Sentry client factory that uses the provided DSN.
func NewSentryClientFromDSN(dsn string) SentryClientFactory {
	return func() (*sentry.Client, error) {
		return sentry.NewClient(sentry.ClientOptions{
			Dsn: dsn,
		})
	}
}

// NewSentryClientFromClient creates a new Sentry client factory that returns the provided client.
func NewSentryClientFromClient(client *sentry.Client) SentryClientFactory {
	return func() (*sentry.Client, error) {
		return client, nil
	}
}

// SentryClientFactory is a function that creates a new Sentry client.
type SentryClientFactory func() (*sentry.Client, error)

// NewScopeFromScope creates a new zapcore.Field that holds the provided Sentry scope.
func NewScopeFromScope(scope *sentry.Scope) zapcore.Field {
	return zapcore.Field{
		Key:       sentryZapScopeKey,
		Type:      zapcore.SkipType,
		Interface: scope,
	}
}

// NewScope creates a new zapcore.Field that holds a new Sentry scope.
func NewScope() zapcore.Field {
	return NewScopeFromScope(sentry.NewScope())
}

// NewCore creates a new zapcore.Core that sends logs to Sentry.
func NewCore(cfg Configuration, factory SentryClientFactory) (zapcore.Core, error) {
	client, err := factory()
	if err != nil {
		return zapcore.NewNopCore(), fmt.Errorf("failed to create Sentry client: %w", err)
	}

	setDefaultConfig(&cfg)

	if cfg.EnableBreadcrumbs && zapcore.LevelOf(cfg.BreadcrumbLevel) > zapcore.LevelOf(cfg.Level) {
		return zapcore.NewNopCore(), fmt.Errorf("invalid configuration: %w", ErrInvalidBreadcrumbLevel)
	}

	core := &core{
		client: client,
		cfg:    &cfg,
		LevelEnabler: &LevelEnabler{
			LevelEnabler:      cfg.Level,
			breadcrumbsLevel:  cfg.BreadcrumbLevel,
			enableBreadcrumbs: cfg.EnableBreadcrumbs,
		},
		flushTimeout: cfg.FlushTimeout,
		fields:       make(map[string]any),
	}

	return core, nil
}

// LevelEnabler is a zapcore.LevelEnabler that also enables breadcrumbs.
type LevelEnabler struct {
	zapcore.LevelEnabler
	enableBreadcrumbs bool
	breadcrumbsLevel  zapcore.LevelEnabler
}

// Enabled returns true if the given level is at or above the configured level.
func (l *LevelEnabler) Enabled(lvl zapcore.Level) bool {
	return l.LevelEnabler.Enabled(lvl) || (l.enableBreadcrumbs && l.breadcrumbsLevel.Enabled(lvl))
}

var levelMap = map[zapcore.Level]sentry.Level{
	zapcore.DebugLevel:  sentry.LevelDebug,
	zapcore.InfoLevel:   sentry.LevelInfo,
	zapcore.WarnLevel:   sentry.LevelWarning,
	zapcore.ErrorLevel:  sentry.LevelError,
	zapcore.DPanicLevel: sentry.LevelFatal,
	zapcore.PanicLevel:  sentry.LevelFatal,
	zapcore.FatalLevel:  sentry.LevelFatal,
}

// AttachCoreToLogger attaches the Sentry core to the provided logger.
func AttachCoreToLogger(sentryCore zapcore.Core, l *zap.Logger) *zap.Logger {
	return l.WithOptions(zap.WrapCore(func(core zapcore.Core) zapcore.Core {
		return zapcore.NewTee(core, sentryCore)
	}))
}

// Configuration is a minimal set of parameters for Sentry integration.
type Configuration struct {
	// Tags is a map of key-value pairs that will be added directly to the sentry.Event tags.
	Tags map[string]string

	// LoggerNameKey specifies the key used to represent the zap logger name in the sentry.Event.
	// If left empty, this feature is disabled.
	LoggerNameKey string

	// DisableStacktrace, when set to true, prevents the addition of stack traces to sentry.Event instances.
	DisableStacktrace bool

	// Level defines the minimum severity level required for events to be sent to Sentry.
	Level zapcore.LevelEnabler

	// EnableBreadcrumbs enables the usage of sentry.Breadcrumb instances, which log application events
	// leading up to a captured Sentry event. This feature requires an explicitly passed new scope.
	EnableBreadcrumbs bool

	// BreadcrumbLevel defines the minimum severity level for breadcrumbs to be recorded.
	// Breadcrumbs represent events that occurred prior to a Sentry event.
	// This field is ignored if EnableBreadcrumbs is false.
	// Note: NewCore will fail if BreadcrumbLevel is greater than Level.
	BreadcrumbLevel zapcore.LevelEnabler

	// MaxBreadcrumbs specifies the maximum number of breadcrumb events to retain.
	// Set to zero or a negative value to use the default limit.
	// This field is ignored if EnableBreadcrumbs is false.
	MaxBreadcrumbs int

	// FlushTimeout defines the maximum duration allowed for flushing events to Sentry.
	FlushTimeout time.Duration

	// Hub overrides the default sentry.CurrentHub.
	// For more information, see the sentry.Hub documentation.
	Hub *sentry.Hub

	// FrameMatcher allows certain frames in stack traces to be ignored.
	// This is particularly useful for excluding frames from utility or wrapper functions
	// that do not provide meaningful context for error analysis.
	FrameMatcher FrameMatcher
}
