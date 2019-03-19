package sentry

import "fmt"

// TODO: Test whether we need locks for all the setters

// TODO: Correct User struct
type User struct {
	id string
}

// TODO: Correct Breadcrumb struct
type Breadcrumb struct {
	message string
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

type Scope struct {
	breadcrumbs     []Breadcrumb
	user            User
	tags            map[string]string
	extra           map[string]interface{}
	fingerprint     []string
	level           Level
	eventProcessors []EventProcessor
}

func NewScope() *Scope {
	return &Scope{
		breadcrumbs: []Breadcrumb{},
		user:        User{},
		tags:        make(map[string]string),
		extra:       make(map[string]interface{}),
		fingerprint: []string{},
		level:       LevelInfo,
	}
}

func (scope *Scope) AddBreadcrumb(breadcrumb Breadcrumb) {
	// TODO: Pull from client.config.maxBreadcrumbs
	const limit = 100
	breadcrumbs := append(scope.breadcrumbs, breadcrumb)
	if len(breadcrumbs) > limit {
		scope.breadcrumbs = breadcrumbs[1 : limit+1]
	} else {
		scope.breadcrumbs = breadcrumbs
	}
}

func (scope *Scope) SetUser(user User) {
	scope.user = user
}

func (scope *Scope) SetTag(key, value string) {
	scope.tags[key] = value
}

func (scope *Scope) SetTags(tags map[string]string) {
	for k, v := range tags {
		scope.tags[k] = v
	}
}

func (scope *Scope) SetExtra(key string, value interface{}) {
	scope.extra[key] = value
}

func (scope *Scope) SetExtras(extra map[string]interface{}) {
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

func (scope Scope) Clone() *Scope {
	clone := NewScope()
	clone.user = scope.user
	clone.breadcrumbs = make([]Breadcrumb, len(scope.breadcrumbs))
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
	*scope = *NewScope()
}

func (scope *Scope) AddEventProcessor(processor EventProcessor) {
	if scope.eventProcessors == nil {
		scope.eventProcessors = []EventProcessor{}
	}
	scope.eventProcessors = append(scope.eventProcessors, processor)
}

func (scope Scope) ApplyToEvent(event *Event) *Event {
	// TODO: Limit to maxBreadcrums
	if len(scope.breadcrumbs) > 0 {
		event.breadcrumbs = append(event.breadcrumbs, scope.breadcrumbs...)
	}

	if scope.extra != nil && len(scope.extra) > 0 {
		if event.extra == nil {
			event.extra = make(map[string]interface{})
		}

		for key, value := range scope.extra {
			event.extra[key] = value
		}
	}

	if scope.tags != nil && len(scope.tags) > 0 {
		if event.tags == nil {
			event.tags = make(map[string]string)
		}

		for key, value := range scope.tags {
			event.tags[key] = value
		}
	}

	if (event.user == User{}) {
		event.user = scope.user
	}

	if event.fingerprint == nil && scope.fingerprint != nil {
		event.fingerprint = make([]string, len(scope.fingerprint))
		copy(event.fingerprint, scope.fingerprint)
	}

	if scope.level != "" {
		event.level = scope.level
	}

	for _, processor := range scope.eventProcessors {
		id := event.eventID
		event = processor(event)
		if event == nil {
			// TODO: Add debug
			fmt.Printf("event processor dropped event %s\n", id)
			return nil
		}
	}

	return event
}
