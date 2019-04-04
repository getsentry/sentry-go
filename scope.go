package sentry

import (
	"time"
)

// TODO: Test whether we need locks for all the setters

// TODO: Correct User struct
type User struct {
	ID string `json:"id"`
}

type BreadcrumbHint map[string]interface{}

// TODO: Correct Breadcrumb struct
type Breadcrumb struct {
	Message   string `json:"message"`
	Timestamp int64  `json:"timestamp"`
}

type EventProcessor func(event *Event) *Event

type Level string

const (
	LevelDebug   Level = "debug"
	LevelInfo    Level = "info"
	LevelWarning Level = "warning"
	LevelError   Level = "error"
	LevelFatal   Level = "fatal"
)

type Scoper interface {
	AddBreadcrumb(breadcrumb *Breadcrumb)
	ApplyToEvent(event *Event) *Event
}

type Scope struct {
	breadcrumbs     []*Breadcrumb
	user            User
	tags            map[string]string
	extra           map[string]interface{}
	fingerprint     []string
	level           Level
	eventProcessors []EventProcessor
}

// TODO: Pull from client.config.maxBreadcrumbs
const breadcrumbsLimit = 100

func (scope *Scope) AddBreadcrumb(breadcrumb *Breadcrumb) {
	if breadcrumb.Timestamp == 0 {
		breadcrumb.Timestamp = time.Now().Unix()
	}

	if scope.breadcrumbs == nil {
		scope.breadcrumbs = []*Breadcrumb{}
	}

	breadcrumbs := append(scope.breadcrumbs, breadcrumb)
	if len(breadcrumbs) > breadcrumbsLimit {
		// Remove the oldest breadcrumb
		scope.breadcrumbs = breadcrumbs[1 : breadcrumbsLimit+1]
	} else {
		scope.breadcrumbs = breadcrumbs
	}
}

func (scope *Scope) SetUser(user User) {
	scope.user = user
}

func (scope *Scope) SetTag(key, value string) {
	if scope.tags == nil {
		scope.tags = make(map[string]string)
	}
	scope.tags[key] = value
}

func (scope *Scope) SetTags(tags map[string]string) {
	if scope.tags == nil {
		scope.tags = make(map[string]string)
	}
	for k, v := range tags {
		scope.tags[k] = v
	}
}

func (scope *Scope) SetExtra(key string, value interface{}) {
	if scope.extra == nil {
		scope.extra = make(map[string]interface{})
	}
	scope.extra[key] = value
}

func (scope *Scope) SetExtras(extra map[string]interface{}) {
	if scope.extra == nil {
		scope.extra = make(map[string]interface{})
	}
	for k, v := range extra {
		scope.extra[k] = v
	}
}

func (scope *Scope) SetFingerprint(fingerprint []string) {
	scope.fingerprint = fingerprint
}

func (scope *Scope) SetLevel(level Level) {
	scope.level = level
}

func (scope *Scope) Clone() *Scope {
	clone := &Scope{
		extra: make(map[string]interface{}),
		tags:  make(map[string]string),
	}
	clone.user = scope.user
	clone.breadcrumbs = make([]*Breadcrumb, len(scope.breadcrumbs))
	copy(clone.breadcrumbs, scope.breadcrumbs)
	for key, value := range scope.extra {
		clone.extra[key] = value
	}
	for key, value := range scope.tags {
		clone.tags[key] = value
	}
	clone.fingerprint = make([]string, len(scope.fingerprint))
	copy(clone.fingerprint, scope.fingerprint)
	clone.level = scope.level
	return clone
}

func (scope *Scope) Clear() {
	*scope = Scope{}
}

func (scope *Scope) ClearBreadcrumbs() {
	scope.breadcrumbs = []*Breadcrumb{}
}

func (scope *Scope) AddEventProcessor(processor EventProcessor) {
	if scope.eventProcessors == nil {
		scope.eventProcessors = []EventProcessor{}
	}
	scope.eventProcessors = append(scope.eventProcessors, processor)
}

func (scope *Scope) ApplyToEvent(event *Event) *Event {
	// TODO: Limit to maxBreadcrums
	if scope.breadcrumbs != nil && len(scope.breadcrumbs) > 0 {
		if event.Breadcrumbs == nil {
			event.Breadcrumbs = []*Breadcrumb{}
		}

		event.Breadcrumbs = append(event.Breadcrumbs, scope.breadcrumbs...)
	}

	if len(event.Breadcrumbs) > breadcrumbsLimit {
		event.Breadcrumbs = event.Breadcrumbs[0:breadcrumbsLimit]
	}

	if scope.extra != nil && len(scope.extra) > 0 {
		if event.Extra == nil {
			event.Extra = make(map[string]interface{})
		}

		for key, value := range scope.extra {
			event.Extra[key] = value
		}
	}

	if scope.tags != nil && len(scope.tags) > 0 {
		if event.Tags == nil {
			event.Tags = make(map[string]string)
		}

		for key, value := range scope.tags {
			event.Tags[key] = value
		}
	}

	if (event.User == User{}) {
		event.User = scope.user
	}

	if (event.Fingerprint == nil || len(event.Fingerprint) == 0) &&
		(scope.fingerprint != nil && len(scope.fingerprint) > 0) {
		event.Fingerprint = make([]string, len(scope.fingerprint))
		copy(event.Fingerprint, scope.fingerprint)
	}

	if scope.level != "" {
		event.Level = scope.level
	}

	for _, processor := range scope.eventProcessors {
		id := event.EventID
		event = processor(event)
		if event == nil {
			debugger.Printf("event processor dropped event %s\n", id)
			return nil
		}
	}

	return event
}
