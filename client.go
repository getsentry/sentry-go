package sentry

import (
	"context"
	"crypto/x509"
	"io"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"time"
)

type Integration interface {
	Name() string
	SetupOnce()
}

type ClientOptions struct {
	Dsn              string
	Debug            bool
	SampleRate       float32
	BeforeSend       func(event *Event, hint *EventHint) *Event
	BeforeBreadcrumb func(breadcrumb *Breadcrumb, hint *BreadcrumbHint) *Breadcrumb
	Integrations     []Integration
	Transport        Transport
	ServerName       string
	Release          string
	Dist             string
	Environment      string
	MaxBreadcrumbs   int
	DebugWriter      io.Writer

	HTTPTransport *http.Transport
	HTTPProxy     string
	HTTPSProxy    string
	CaCerts       *x509.CertPool
}

type Client struct {
	options      ClientOptions
	dsn          *Dsn
	integrations map[string]Integration
	Transport    Transport
}

// Or client.Configure which would allow us to keep most data on struct private
func NewClient(options ClientOptions) (*Client, error) {
	if options.Debug {
		debugWriter := options.DebugWriter
		if debugWriter == nil {
			debugWriter = os.Stdout
		}
		debugger.SetOutput(debugWriter)
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
		debugger.Println("Sentry client initialized with an empty DSN")
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
		transport = new(HTTPTransport)
	}

	transport.Configure(client.options)
	client.Transport = transport
}

func (client *Client) setupIntegrations() {
	if client.options.Integrations == nil {
		return
	}

	client.integrations = make(map[string]Integration)

	for _, integration := range client.options.Integrations {
		client.integrations[integration.Name()] = integration
		integration.SetupOnce()
		debugger.Printf("Integration installed: %s\n", integration.Name())
	}
}

func (client Client) Options() ClientOptions {
	return client.options
}

func (client *Client) CaptureMessage(message string, hint *EventHint, scope EventModifier) {
	event := client.eventFromMessage(message)
	client.CaptureEvent(event, hint, scope)
}

func (client *Client) CaptureException(exception error, hint *EventHint, scope EventModifier) {
	event := client.eventFromException(exception)
	client.CaptureEvent(event, hint, scope)
}

func (client *Client) CaptureEvent(event *Event, hint *EventHint, scope EventModifier) {
	client.processEvent(event, hint, scope)
}

func (client *Client) Recover(recoveredErr interface{}, hint *EventHint, scope EventModifier) {
	if recoveredErr == nil {
		recoveredErr = recover()
	}

	if recoveredErr != nil {
		if err, ok := recoveredErr.(error); ok {
			client.CaptureException(err, hint, scope)
		}

		if err, ok := recoveredErr.(string); ok {
			client.CaptureMessage(err, hint, scope)
		}
	}
}

func (client *Client) RecoverWithContext(ctx context.Context, err interface{}, hint *EventHint, scope EventModifier) {
	if err == nil {
		err = recover()
	}

	if err != nil {
		if hint.Context == nil && ctx != nil {
			hint.Context = ctx
		}

		if err, ok := err.(error); ok {
			client.CaptureException(err, hint, scope)
		}

		if err, ok := err.(string); ok {
			client.CaptureMessage(err, hint, scope)
		}
	}
}

func (client *Client) Flush(timeout time.Duration) bool {
	return client.Transport.Flush(timeout)
}

func (client *Client) eventFromMessage(message string) *Event {
	return &Event{
		Message: message,
	}
}

func (client *Client) eventFromException(exception error) *Event {
	// TODO: Extract stacktrace from the exception
	return &Event{
		Message: exception.Error(),
	}
}

// TODO: Should return some sort of SentryResponse instead of http.Response
func (client *Client) processEvent(event *Event, hint *EventHint, scope EventModifier) {
	options := client.Options()

	// TODO: Reconsider if its worth going away from default implementation
	// of other SDKs. In Go zero value (default) for float32 is 0.0,
	// which means that if someone uses ClientOptions{} struct directly
	// and we would not check for 0 here, we'd skip all events by default
	if options.SampleRate != 0.0 {
		randomFloat := rand.New(rand.NewSource(time.Now().UnixNano())).Float32()
		if randomFloat > options.SampleRate {
			debugger.Println("event dropped due to SampleRate hit")
			return
		}
	}

	if event = client.prepareEvent(event, hint, scope); event == nil {
		debugger.Println("event dropped by one of the EventProcessors")
		return
	}

	if options.BeforeSend != nil {
		h := &EventHint{}
		if hint != nil {
			h = hint
		}
		if event = options.BeforeSend(event, h); event == nil {
			debugger.Println("event dropped due to BeforeSend callback")
			return
		}
	}

	client.Transport.SendEvent(event)
}

func (client *Client) prepareEvent(event *Event, hint *EventHint, scope EventModifier) *Event {
	// TODO: Set all the defaults, clear unnecessary stuff etc. here

	if event.EventID == "" {
		event.EventID = uuid()
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

	event.Sdk = SdkInfo{
		Name:         "sentry.go",
		Version:      VERSION,
		Integrations: client.listIntegrations(),
		Packages: []SdkPackage{{
			Name:    "sentry-go",
			Version: VERSION,
		}},
	}
	event.Platform = "go"
	event.Transaction = "Don't sneak into my computer please"

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
