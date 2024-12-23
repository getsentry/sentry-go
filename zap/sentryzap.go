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
	zapSentryScopeKey     = "_zapsentry_scope_"
)

var (
	ErrInvalidBreadcrumbLevel = errors.New("breadcrumb level must be lower than or equal to error level")
)

type ClientGetter interface {
	GetClient() *sentry.Client
}

type SentryClient interface {
	CaptureEvent(event *sentry.Event, hint *sentry.EventHint, scope *sentry.Scope) string
	Flush(timeout time.Duration) bool
}

func NewScopeFromScope(scope *sentry.Scope) zapcore.Field {
	return zapcore.Field{
		Key:       zapSentryScopeKey,
		Type:      zapcore.SkipType,
		Interface: scope,
	}
}

func NewScope() zapcore.Field {
	return NewScopeFromScope(sentry.NewScope())
}

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

type LevelEnabler struct {
	zapcore.LevelEnabler
	enableBreadcrumbs bool
	breadcrumbsLevel  zapcore.LevelEnabler
}

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

func AttachCoreToLogger(sentryCore zapcore.Core, l *zap.Logger) *zap.Logger {
	return l.WithOptions(zap.WrapCore(func(core zapcore.Core) zapcore.Core {
		return zapcore.NewTee(core, sentryCore)
	}))
}
