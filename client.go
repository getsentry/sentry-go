package sentry

import (
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/google/uuid"
)

const SdkName string = "sentry.go"
const SdkVersion string = "0.0.0-beta"
const SdkUserAgent string = SdkName + "/" + SdkVersion

type LogEntry struct {
	Message string
	Params  []interface{}
}

type Request struct{}

type Exception struct{}

type Context string

type Event struct {
	EventID     uuid.UUID              `json:"event_id"`
	Level       Level                  `json:"level"`
	Message     string                 `json:"message"`
	Fingerprint []string               `json:"fingerprint"`
	Timestamp   int64                  `json:"timestamp"`
	User        User                   `json:"user"`
	Breadcrumbs []*Breadcrumb          `json:"breadcrumbs"`
	Tags        map[string]string      `json:"tags"`
	Extra       map[string]interface{} `json:"extra"` // TODO: Should it be more strict?
	Sdk         ClientSdkInfo          `json:"sdk"`
	Transaction string                 `json:"transaction"`
	ServerName  string                 `json:"server_name"`
	Release     string                 `json:"release"`
	Dist        string                 `json:"dist"`
	Environment string                 `json:"environment"`
	// Logentry    LogEntry
	// Logger      string
	// Modules     map[string]string
	// Platform    string
	// Request     Request
	// Contexts    map[string]Context
	// Exception   []Exception
}

type ClientSdkInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type ClientOptions struct {
	Dsn         string
	Debug       bool
	SampleRate  float32
	BeforeSend  func(event *Event) *Event
	ServerName  string
	Release     string
	Dist        string
	Environment string
}

type Clienter interface {
	AddBreadcrumb(breadcrumb *Breadcrumb, scope Scoper)
	CaptureMessage(message string, scope Scoper)
	CaptureException(exception error, scope Scoper)
	CaptureEvent(event *Event, scope Scoper)
}

type Client struct {
	Options ClientOptions
	Dsn     *Dsn
}

func NewClient(options ClientOptions) (*Client, error) {
	if options.Debug {
		debugger.Enable()
	}

	dsn, err := NewDsn(options.Dsn)

	if err != nil {
		return nil, err
	}

	if dsn == nil {
		debugger.Println("Sentry client initialized with an empty DSN")
	}

	return &Client{
		Options: options,
		Dsn:     dsn,
	}, nil
}

func (client *Client) AddBreadcrumb(breadcrumb *Breadcrumb, scope Scoper) {
	scope.AddBreadcrumb(breadcrumb)
}

func (client *Client) CaptureMessage(message string, scope Scoper) {
	event := client.eventFromMessage(message)
	client.CaptureEvent(event, scope)
}

func (client *Client) CaptureException(exception error, scope Scoper) {
	event := client.eventFromException(exception)
	client.CaptureEvent(event, scope)
}

func (client *Client) CaptureEvent(event *Event, scope Scoper) {
	if err := client.processEvent(event, scope); err != nil {
		debugger.Println(err)
	}
	log.Println("TODO[CaptureEvent]: Handle return values")
}

func (client *Client) eventFromMessage(message string) *Event {
	return &Event{
		Message: message,
	}
}

func (client *Client) eventFromException(exception error) *Event {
	log.Println("TODO[CaptureException]: Extract stacktrace from the exception")
	return &Event{
		Message: exception.Error(),
	}
}

// TODO: Should return some sort of SentryResponse instead of http.Response
func (client *Client) processEvent(event *Event, scope Scoper) error {
	options := client.Options

	// TODO: Reconsider if its worth going away from default implementation
	// of other SDKs. In Go zero value (default) for float32 is 0.0,
	// which means that if someone uses ClientOptions{} struct directly
	// and we would not check for 0 here, we'd skip all events by default
	if options.SampleRate != 0.0 {
		randomFloat := rand.New(rand.NewSource(time.Now().UnixNano())).Float32()
		if randomFloat > options.SampleRate {
			return fmt.Errorf("event dropped due to SampleRate hit")
		}
	}

	if event = client.prepareEvent(event, scope); event == nil {
		return fmt.Errorf("event dropped by one of the EventProcessors")
	}

	if options.BeforeSend != nil {
		if event = options.BeforeSend(event); event == nil {
			return fmt.Errorf("event dropped due to BeforeSend callback")
		}
	}

	return nil
}

func (client *Client) prepareEvent(event *Event, scope Scoper) *Event {
	// TODO: Set all the defaults, clear unnecessary stuff etc. here

	var emptyEventID uuid.UUID
	if event.EventID == emptyEventID {
		event.EventID = uuid.New()
	}

	if event.Timestamp == 0 {
		event.Timestamp = time.Now().Unix()
	}

	if event.Level == "" {
		event.Level = LevelInfo
	}

	event.Sdk = ClientSdkInfo{
		Name:    SdkName,
		Version: SdkVersion,
	}

	event.Transaction = "Don't sneak into my computer please"

	return scope.ApplyToEvent(event)
}
