package sentry

import (
	"context"
	"crypto/x509"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"reflect"
	"sort"
	"time"
)

// Logger is an instance of log.Logger that is use to provide debug information about running Sentry Client
// can be enabled by either using `Logger.SetOutput` directly or with `Debug` client option
var Logger = log.New(ioutil.Discard, "[Sentry] ", log.LstdFlags)

// Integration allows for registering a functions that modify or discard captured events.
type Integration interface {
	Name() string
	SetupOnce()
}

// ClientOptions that configures a SDK Client
type ClientOptions struct {
	// The DSN to use. If not set the client is effectively disabled.
	Dsn string
	// In debug mode debug information is printed to stdput to help you understand what
	// sentry is doing.
	Debug bool
	// The sample rate for event submission (0.0 - 1.0, defaults to 1.0)
	SampleRate float32
	// Before send callback.
	BeforeSend func(event *Event, hint *EventHint) *Event
	// Before breadcrumb add callback.
	BeforeBreadcrumb func(breadcrumb *Breadcrumb, hint *BreadcrumbHint) *Breadcrumb
	// Integrations to be installed on the current Client, receives default integrations
	Integrations func([]Integration) []Integration
	// io.Writer implementation that should be used with the `Debug` mode
	DebugWriter io.Writer
	// The transport to use.
	// This is an instance of a struct implementing `Transport` interface.
	// Defaults to `httpTransport` from `transport.go`
	Transport Transport
	// The server name to be reported.
	ServerName string
	// The release to be sent with events.
	Release string
	// The dist to be sent with events.
	Dist string
	// The environment to be sent with events.
	Environment string
	// Maximum number of breadcrumbs.
	MaxBreadcrumbs int
	// An optional pointer to `http.Transport` that will be used with a default HTTPTransport.
	HTTPTransport *http.Transport
	// An optional HTTP proxy to use.
	// This will default to the `http_proxy` environment variable.
	// or `https_proxy` if that one exists.
	HTTPProxy string
	// An optional HTTPS proxy to use.
	// This will default to the `HTTPS_PROXY` environment variable
	// or `http_proxy` if that one exists.
	HTTPSProxy string
	// An optionsl CaCerts to use.
	// Defaults to `gocertifi.CACerts()`.
	CaCerts *x509.CertPool
}

// Client is the underlying processor that's used by the main API and `Hub` instances.
type Client struct {
	options      ClientOptions
	dsn          *Dsn
	integrations map[string]Integration
	Transport    Transport
}

// NewClient creates and returns an instance of `Client` configured using `ClientOptions`.
func NewClient(options ClientOptions) (*Client, error) {
	if options.Debug {
		debugWriter := options.DebugWriter
		if debugWriter == nil {
			debugWriter = os.Stdout
		}
		Logger.SetOutput(debugWriter)
	}

	if options.Dsn == "" {
		options.Dsn = os.Getenv("SENTRY_DSN")
	}

	if options.Release == "" {
		options.Release = os.Getenv("SENTRY_RELEASE")
	}

	if options.Environment == "" {
		options.Environment = os.Getenv("SENTRY_ENVIRONMENT")
	}

	dsn, err := NewDsn(options.Dsn)

	if err != nil {
		return nil, err
	}

	if dsn == nil {
		Logger.Println("Sentry client initialized with an empty DSN")
	}

	client := Client{
		options: options,
		dsn:     dsn,
	}

	client.setupTransport()
	client.setupIntegrations()

	return &client, nil
}

func (client *Client) setupTransport() {
	transport := client.options.Transport

	if transport == nil {
		transport = new(httpTransport)
	}

	transport.Configure(client.options)
	client.Transport = transport
}

func (client *Client) setupIntegrations() {
	integrations := []Integration{
		new(environmentIntegration),
		new(modulesIntegration),
		new(requestIntegration),
	}

	if client.options.Integrations != nil {
		integrations = client.options.Integrations(integrations)
	}

	client.integrations = make(map[string]Integration)

	for _, integration := range integrations {
		if _, ok := client.integrations[integration.Name()]; ok {
			Logger.Printf("Integration %s is already installed\n", integration.Name())
			continue
		}
		client.integrations[integration.Name()] = integration
		integration.SetupOnce()
		Logger.Printf("Integration installed: %s\n", integration.Name())
	}
}

// Options return `ClientOptions` for the current `Client`.
func (client Client) Options() ClientOptions {
	return client.options
}

// CaptureMessage captures an arbitrary message.
func (client *Client) CaptureMessage(message string, hint *EventHint, scope EventModifier) *EventID {
	event := client.eventFromMessage(message, LevelInfo)
	return client.CaptureEvent(event, hint, scope)
}

// CaptureException captures an error.
func (client *Client) CaptureException(exception error, hint *EventHint, scope EventModifier) *EventID {
	event := client.eventFromException(exception, LevelError)
	return client.CaptureEvent(event, hint, scope)
}

// CaptureEvent captures an event on the currently active client if any.
//
// The event must already be assembled. Typically code would instead use
// the utility methods like `CaptureException`. The return value is the
// event ID. In case Sentry is disabled or event was dropped, the return value will be nil.
func (client *Client) CaptureEvent(event *Event, hint *EventHint, scope EventModifier) *EventID {
	return client.processEvent(event, hint, scope)
}

// Recover captures a panic.
func (client *Client) Recover(err interface{}, hint *EventHint, scope EventModifier) {
	if err == nil {
		err = recover()
	}

	if err != nil {
		if err, ok := err.(error); ok {
			event := client.eventFromException(err, LevelFatal)
			client.CaptureEvent(event, hint, scope)
		}

		if err, ok := err.(string); ok {
			event := client.eventFromMessage(err, LevelFatal)
			client.CaptureEvent(event, hint, scope)
		}
	}
}

// Recover captures a panic and passes relevant context object.
func (client *Client) RecoverWithContext(ctx context.Context, err interface{}, hint *EventHint, scope EventModifier) {
	if err == nil {
		err = recover()
	}

	if err != nil {
		if hint.Context == nil && ctx != nil {
			hint.Context = ctx
		}

		if err, ok := err.(error); ok {
			event := client.eventFromException(err, LevelFatal)
			client.CaptureEvent(event, hint, scope)
		}

		if err, ok := err.(string); ok {
			event := client.eventFromMessage(err, LevelFatal)
			client.CaptureEvent(event, hint, scope)
		}
	}
}

// Flush notifies when all the buffered events have been sent by returning `true`
// or `false` if timeout was reached. It calls `Flush` method of the configured `Transport`.
func (client *Client) Flush(timeout time.Duration) bool {
	return client.Transport.Flush(timeout)
}

func (client *Client) eventFromMessage(message string, level Level) *Event {
	return &Event{
		Level:   level,
		Message: message,
	}
}

func (client *Client) eventFromException(exception error, level Level) *Event {
	stacktrace := ExtractStacktrace(exception)

	if stacktrace == nil {
		stacktrace = NewStacktrace()
	}

	return &Event{
		Level: level,
		Exception: []Exception{{
			Value:      exception.Error(),
			Type:       reflect.TypeOf(exception).String(),
			Stacktrace: stacktrace,
		}},
	}
}

func (client *Client) processEvent(event *Event, hint *EventHint, scope EventModifier) *EventID {
	options := client.Options()

	// TODO: Reconsider if its worth going away from default implementation
	// of other SDKs. In Go zero value (default) for float32 is 0.0,
	// which means that if someone uses ClientOptions{} struct directly
	// and we would not check for 0 here, we'd skip all events by default
	if options.SampleRate != 0.0 {
		randomFloat := rand.New(rand.NewSource(time.Now().UnixNano())).Float32()
		if randomFloat > options.SampleRate {
			Logger.Println("event dropped due to SampleRate hit")
			return nil
		}
	}

	if event = client.prepareEvent(event, hint, scope); event == nil {
		Logger.Println("event dropped by one of the EventProcessors")
		return nil
	}

	if options.BeforeSend != nil {
		h := &EventHint{}
		if hint != nil {
			h = hint
		}
		if event = options.BeforeSend(event, h); event == nil {
			Logger.Println("event dropped due to BeforeSend callback")
			return nil
		}
	}

	client.Transport.SendEvent(event)

	return &event.EventID
}

func (client *Client) prepareEvent(event *Event, hint *EventHint, scope EventModifier) *Event {
	if event.EventID == "" {
		event.EventID = EventID(uuid())
	}

	if event.Timestamp == 0 {
		event.Timestamp = time.Now().Unix()
	}

	if event.Level == "" {
		event.Level = LevelInfo
	}

	if event.ServerName == "" {
		if hostname, err := os.Hostname(); err == nil {
			event.ServerName = hostname
		}
	}

	event.Platform = "go"
	event.Sdk = SdkInfo{
		Name:         "sentry.go",
		Version:      Version,
		Integrations: client.listIntegrations(),
		Packages: []SdkPackage{{
			Name:    "sentry-go",
			Version: Version,
		}},
	}

	return scope.ApplyToEvent(event, hint)
}

func (client Client) listIntegrations() []string {
	integrations := make([]string, 0, len(client.integrations))
	for key := range client.integrations {
		integrations = append(integrations, key)
	}
	sort.Strings(integrations)
	return integrations
}
