package sentry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/getsentry/sentry-go/attribute"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/require"
)

const sharedContextsKey = "sharedContextsKey"

var testNow = time.Now().UTC()

func fillScopeWithData(scope *Scope) *Scope {
	scope.breadcrumbs = []*Breadcrumb{{Timestamp: testNow, Message: "scopeBreadcrumbMessage"}}
	scope.attachments = []*Attachment{
		{
			Filename: "scope-attachment.txt",
			Payload:  []byte("Scope attachment contents."),
		},
	}
	scope.user = User{ID: "1337"}
	scope.tags = map[string]string{"scopeTagKey": "scopeTagValue"}
	scope.contexts = map[string]Context{
		"scopeContextsKey": {"scopeContextKey": "scopeContextValue"},
		sharedContextsKey:  {"scopeContextKey": "scopeContextValue"},
	}
	scope.attributes = map[string]attribute.Value{"scopeAttributeKey": attribute.StringValue("scopeAttributeValue")}
	scope.fingerprint = []string{"scopeFingerprintOne", "scopeFingerprintTwo"}
	scope.level = LevelDebug
	scope.request = httptest.NewRequest("GET", "/wat", nil)
	scope.propagationContext = NewPropagationContext()
	return scope
}

func TestScopeUserDataIsolation(t *testing.T) {
	t.Parallel()

	data := map[string]string{"role": "original"}
	parent := NewScope()
	parent.SetUser(User{Data: data})
	child := parent.Clone()
	data["role"] = "parent changed"
	assertEqual(t, "original", child.ApplyToEvent(NewEvent(), nil, nil).User.Data["role"])

	mutate := true
	child.AddEventProcessor(func(event *Event, _ *EventHint) *Event {
		if mutate {
			mutate = false
			event.User.Data["role"] = "event changed"
		}
		return event
	})
	assertEqual(t, "event changed", child.ApplyToEvent(NewEvent(), nil, nil).User.Data["role"])
	assertEqual(t, "original", child.ApplyToEvent(NewEvent(), nil, nil).User.Data["role"])
	assertEqual(t, "parent changed", parent.ApplyToEvent(NewEvent(), nil, nil).User.Data["role"])
}

func TestScopeSettersOnCapturedEvent(t *testing.T) {
	type fields struct {
		Tags        map[string]string
		Contexts    map[string]Context
		Level       Level
		Fingerprint []string
		UserID      string
	}
	for _, test := range []struct {
		name  string
		setup func(*Scope)
		want  fields
	}{
		{
			name: "SetTags merges into existing tags",
			setup: func(s *Scope) {
				s.SetTag("kept", "k")
				s.SetTag("replaced", "old")
				s.SetTags(map[string]string{"replaced": "new", "added": "a"})
			},
			want: fields{Level: LevelInfo, Tags: map[string]string{"kept": "k", "replaced": "new", "added": "a"}},
		},
		{
			name: "SetContexts merges into existing contexts",
			setup: func(s *Scope) {
				s.SetContext("kept", Context{"k": "k"})
				s.SetContext("replaced", Context{"old": "o"})
				s.SetContexts(map[string]Context{"replaced": {"new": "n"}, "added": {"a": "a"}})
			},
			want: fields{Level: LevelInfo, Contexts: map[string]Context{"kept": {"k": "k"}, "replaced": {"new": "n"}, "added": {"a": "a"}}},
		},
		{
			name: "RemoveTag and RemoveContext drop only their key",
			setup: func(s *Scope) {
				s.SetTags(map[string]string{"kept": "k", "removed": "r"})
				s.SetContexts(map[string]Context{"kept": {"k": "k"}, "removed": {"r": "r"}})
				s.RemoveTag("removed")
				s.RemoveContext("removed")
			},
			want: fields{Level: LevelInfo, Tags: map[string]string{"kept": "k"}, Contexts: map[string]Context{"kept": {"k": "k"}}},
		},
		{
			name: "last level, fingerprint and user win",
			setup: func(s *Scope) {
				s.SetLevel(LevelInfo)
				s.SetLevel(LevelError)
				s.SetFingerprint([]string{"old"})
				s.SetFingerprint([]string{"new"})
				s.SetUser(User{ID: "old"})
				s.SetUser(User{ID: "new"})
			},
			want: fields{Level: LevelError, Fingerprint: []string{"new"}, UserID: "new"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			cleanGlobalScope(t)
			scope := NewScope()
			test.setup(scope)
			client, transport := newCaptureTestClient(t, ClientOptions{})
			ctx := ContextWithScope(ContextWithClient(context.Background(), client), scope)
			require.NotNil(t, CaptureEvent(ctx, NewEvent()))
			got := requireSingleEvent(t, transport)

			delete(got.Contexts, "trace")
			gotFields := fields{Tags: got.Tags, Contexts: got.Contexts, Level: got.Level, Fingerprint: got.Fingerprint, UserID: got.User.ID}
			if diff := cmp.Diff(test.want, gotFields, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("event (-want +got):\n%s", diff)
			}
		})
	}
}

func TestScopeRemoveAttribute(t *testing.T) {
	scope := NewScope()
	scope.SetAttributes(
		attribute.String("key.one", "val1"),
		attribute.String("key.two", "val2"),
	)
	scope.RemoveAttribute("key.two")

	assertEqual(t, map[string]attribute.Value{
		"key.one": attribute.StringValue("val1"),
	}, scope.attributes)
}

func TestScopeSetContexts(t *testing.T) {
	scope := NewScope()
	scope.SetContexts(map[string]Context{"a": {"b": 1}})

	assertEqual(t, map[string]Context{"a": {"b": 1}}, scope.contexts)
}

func TestScopeRemoveContext(t *testing.T) {
	scope := NewScope()
	scope.SetContext("a", Context{"foo": "foo"})
	scope.SetContext("b", Context{"bar": "bar"})
	scope.RemoveContext("b")

	assertEqual(t, map[string]Context{"a": {"foo": "foo"}}, scope.contexts)
}

func TestCopyToEventReleasesReadLockAfterPanic(t *testing.T) {
	t.Parallel()
	scope := NewScope()
	scope.SetContext("test", Context{"value": "scope"})
	require.Panics(t, func() { scope.ApplyToEvent(nil, nil, NewNoopClient()) })
	require.True(t, scope.mu.TryLock())
	scope.mu.Unlock()
}

func TestAddBreadcrumbDefaultLimit(t *testing.T) {
	scope := NewScope()
	for range 101 {
		scope.AddBreadcrumb(&Breadcrumb{Timestamp: testNow, Message: "test"}, defaultMaxBreadcrumbs)
	}

	if len(scope.breadcrumbs) != 100 {
		t.Error("expected to have only 100 breadcrumbs")
	}
}

func TestAddBreadcrumbAddsTimestamp(t *testing.T) {
	scope := NewScope()
	before := time.Now()
	scope.AddBreadcrumb(&Breadcrumb{Message: "test"}, defaultMaxBreadcrumbs)
	after := time.Now()
	ts := scope.breadcrumbs[0].Timestamp

	if ts.Before(before) || ts.After(after) {
		t.Errorf("expected default timestamp to represent current time, was '%v'", ts)
	}
}

func TestScopeBasicInheritance(t *testing.T) {
	scope := NewScope()
	scope.SetAttributes(attribute.Int("a", 1))
	scope.SetRequestBody([]byte("requestbody"))
	scope.AddEventProcessor(func(event *Event, _ *EventHint) *Event {
		return event
	})
	clone := scope.Clone()

	assertEqual(t, scope.attributes, clone.attributes)
	assertEqual(t, scope.requestBody, clone.requestBody)
	assertEqual(t, scope.eventProcessors, clone.eventProcessors)
}

func TestScopeCloneEventProcessorsIsolation(t *testing.T) {
	var ran []string
	mark := func(id string) EventProcessor {
		return func(event *Event, _ *EventHint) *Event {
			ran = append(ran, id)
			return event
		}
	}

	scope := NewScope()
	scope.AddEventProcessor(mark("a"))
	scope.AddEventProcessor(mark("b"))
	scope.AddEventProcessor(mark("c"))

	clone1 := scope.Clone()
	clone2 := scope.Clone()
	clone1.AddEventProcessor(mark("x"))
	clone2.AddEventProcessor(mark("y"))

	ran = nil
	clone1.ApplyToEvent(NewEvent(), nil, nil)
	assertEqual(t, []string{"a", "b", "c", "x"}, ran)

	ran = nil
	clone2.ApplyToEvent(NewEvent(), nil, nil)
	assertEqual(t, []string{"a", "b", "c", "y"}, ran)

	ran = nil
	scope.ApplyToEvent(NewEvent(), nil, nil)
	assertEqual(t, []string{"a", "b", "c"}, ran)
}

func TestScopeChildOverrideInheritance(t *testing.T) {
	scope := NewScope()

	scope.SetTag("foo", "baz")
	scope.SetContext("foo", Context{"foo": "baz"})
	scope.SetAttributes(attribute.String("foo", "baz"))
	scope.SetLevel(LevelFatal)
	scope.SetFingerprint([]string{"bar"})
	scope.AddBreadcrumb(&Breadcrumb{Timestamp: testNow, Message: "bar"}, defaultMaxBreadcrumbs)
	scope.AddAttachment(&Attachment{Filename: "bar.txt", Payload: []byte("bar")})
	scope.SetUser(User{ID: "bar"})
	r1 := httptest.NewRequest("GET", "/bar", nil)
	scope.SetRequest(r1)
	scope.AddEventProcessor(func(event *Event, _ *EventHint) *Event {
		return event
	})
	p1 := NewPropagationContext()
	scope.SetPropagationContext(p1)

	clone := scope.Clone()
	clone.SetTag("foo", "bar")
	clone.SetContext("foo", Context{"foo": "bar"})
	clone.SetAttributes(attribute.String("foo", "bar"))
	clone.SetLevel(LevelDebug)
	clone.SetFingerprint([]string{"foo"})
	clone.AddBreadcrumb(&Breadcrumb{Timestamp: testNow, Message: "foo"}, defaultMaxBreadcrumbs)
	clone.AddAttachment(&Attachment{Filename: "foo.txt", Payload: []byte("foo")})
	clone.SetUser(User{ID: "foo"})
	r2 := httptest.NewRequest("GET", "/foo", nil)
	clone.SetRequest(r2)
	clone.AddEventProcessor(func(event *Event, _ *EventHint) *Event {
		return event
	})
	p2 := NewPropagationContext()
	clone.SetPropagationContext(p2)

	assertEqual(t, map[string]string{"foo": "bar"}, clone.tags)
	assertEqual(t, map[string]Context{"foo": {"foo": "bar"}}, clone.contexts)
	assertEqual(t, map[string]attribute.Value{"foo": attribute.StringValue("bar")}, clone.attributes)
	assertEqual(t, LevelDebug, clone.level)
	assertEqual(t, []string{"foo"}, clone.fingerprint)
	assertEqual(t, []*Breadcrumb{
		{Timestamp: testNow, Message: "bar"},
		{Timestamp: testNow, Message: "foo"},
	}, clone.breadcrumbs)
	assertEqual(t, []*Attachment{
		{Filename: "bar.txt", Payload: []byte("bar")},
		{Filename: "foo.txt", Payload: []byte("foo")},
	}, clone.attachments)
	assertEqual(t, User{ID: "foo"}, clone.user)
	assertEqual(t, r2, clone.request)
	assertEqual(t, p2, clone.propagationContext)

	assertEqual(t, map[string]string{"foo": "baz"}, scope.tags)
	assertEqual(t, map[string]Context{"foo": {"foo": "baz"}}, scope.contexts)
	assertEqual(t, map[string]attribute.Value{"foo": attribute.StringValue("baz")}, scope.attributes)
	assertEqual(t, LevelFatal, scope.level)
	assertEqual(t, []string{"bar"}, scope.fingerprint)
	assertEqual(t, []*Breadcrumb{{Timestamp: testNow, Message: "bar"}}, scope.breadcrumbs)
	assertEqual(t, []*Attachment{{Filename: "bar.txt", Payload: []byte("bar")}}, scope.attachments)
	assertEqual(t, User{ID: "bar"}, scope.user)
	assertEqual(t, r1, scope.request)
	assertEqual(t, p1, scope.propagationContext)

	assertEqual(t, len(scope.eventProcessors), 1)
	assertEqual(t, len(clone.eventProcessors), 2)
}

func TestClear(t *testing.T) {
	scope := fillScopeWithData(NewScope())
	propagationContext := scope.propagationContextSnapshot()
	processor := func(event *Event, _ *EventHint) *Event { return event }
	scope.AddEventProcessor(processor)
	scope.Clear()

	assertEqual(t, []*Breadcrumb{}, scope.breadcrumbs)
	assertEqual(t, []*Attachment{}, scope.attachments)
	assertEqual(t, User{}, scope.user)
	assertEqual(t, map[string]string{}, scope.tags)
	assertEqual(t, map[string]Context{}, scope.contexts)
	assertEqual(t, map[string]attribute.Value{}, scope.attributes)
	assertEqual(t, []string{}, scope.fingerprint)
	assertEqual(t, Level(""), scope.level)
	assertEqual(t, (*http.Request)(nil), scope.request)
	assertEqual(t, 1, len(scope.eventProcessors))
	assertEqual(t, propagationContext, scope.propagationContextSnapshot())
}

func TestClearAndReconfigure(t *testing.T) {
	scope := fillScopeWithData(NewScope())
	scope.Clear()

	scope.SetTag("foo", "bar")
	scope.SetContext("foo", Context{"foo": "bar"})
	scope.SetAttributes(attribute.String("foo", "bar"))
	scope.SetLevel(LevelDebug)
	scope.SetFingerprint([]string{"foo"})
	scope.AddBreadcrumb(&Breadcrumb{Timestamp: testNow, Message: "foo"}, defaultMaxBreadcrumbs)
	scope.AddAttachment(&Attachment{Filename: "foo.txt", Payload: []byte("foo")})
	scope.SetUser(User{ID: "foo"})
	r := httptest.NewRequest("GET", "/foo", nil)
	scope.SetRequest(r)
	p := NewPropagationContext()
	scope.SetPropagationContext(p)

	assertEqual(t, map[string]string{"foo": "bar"}, scope.tags)
	assertEqual(t, map[string]Context{"foo": {"foo": "bar"}}, scope.contexts)
	assertEqual(t, map[string]attribute.Value{"foo": attribute.StringValue("bar")}, scope.attributes)
	assertEqual(t, LevelDebug, scope.level)
	assertEqual(t, []string{"foo"}, scope.fingerprint)
	assertEqual(t, []*Breadcrumb{{Timestamp: testNow, Message: "foo"}}, scope.breadcrumbs)
	assertEqual(t, []*Attachment{{Filename: "foo.txt", Payload: []byte("foo")}}, scope.attachments)
	assertEqual(t, User{ID: "foo"}, scope.user)
	assertEqual(t, r, scope.request)
	assertEqual(t, p, scope.propagationContext)
}

func TestClearAttachments(t *testing.T) {
	scope := fillScopeWithData(NewScope())
	scope.ClearAttachments()

	assertEqual(t, []*Attachment{}, scope.attachments)
}

func TestApplyToEventUsingEmptyEvent(t *testing.T) {
	scope := fillScopeWithData(NewScope())
	event := NewEvent()

	processedEvent := scope.ApplyToEvent(event, nil, nil)
	assertEqual(t, len(processedEvent.Breadcrumbs), 1, "should use scope breadcrumbs")
	assertEqual(t, len(processedEvent.Attachments), 1, "should use scope attachments")
	assertEqual(t, len(processedEvent.Tags), 1, "should use scope tags")
	assertEqual(t, len(processedEvent.Contexts), 3, "should use scope contexts")
	assertEqual(t, processedEvent.User, scope.user, "should use scope user")
	assertEqual(t, processedEvent.Fingerprint, scope.fingerprint, "should use scope fingerprint")
	assertEqual(t, processedEvent.Level, scope.level, "should use scope level")
	assertEqual(t, processedEvent.Request, NewRequest(scope.request), "should use scope request")
	processedEvent.Contexts["scopeContextsKey"]["scopeContextKey"] = "changed by transport"
	assertEqual(t, "scopeContextValue", scope.contexts["scopeContextsKey"]["scopeContextKey"], "event must not own scope context")
}

func TestApplyToEventUsesClientPIISettingsForRequest(t *testing.T) {
	currentClient, err := NewClient(ClientOptions{Dsn: "https://key@sentry.io/1", SendDefaultPII: true})
	if err != nil {
		t.Fatal(err)
	}
	previousClient := globalClientSnapshot()
	setGlobalClient(currentClient)
	t.Cleanup(func() {
		setGlobalClient(previousClient)
		currentClient.Close()
	})

	scope := NewScope()
	request := httptest.NewRequest("POST", "http://example.com/test", nil)
	request.Header.Set("Authorization", "Bearer secret")
	request.Header.Set("Cookie", "foo=bar")
	request.Header.Set("Some-Header", "some-header value")
	scope.SetRequest(request)

	requestClient, err := NewClient(ClientOptions{
		Dsn: "https://key@sentry.io/1",
		DataCollection: &DataCollection{
			UserInfo: Set(false),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	event := scope.ApplyToEvent(NewEvent(), nil, requestClient)

	if event.Request == nil {
		t.Fatal("expected request to be attached")
	}

	want := &Request{
		URL:         "http://example.com/test",
		Method:      "POST",
		QueryString: "",
		Cookies:     "foo=bar",
		Headers: map[string]string{
			"Authorization": "[Filtered]",
			"Cookie":        "foo=bar",
			"Host":          "example.com",
			"Some-Header":   "some-header value",
		},
		Env: nil,
	}

	assertEqual(t, event.Request, want, "should honor the passed client data collection setting")
}

func TestApplyToEventHTTPBodyCollection(t *testing.T) {
	tests := []struct {
		name     string
		bodies   []BodyType
		body     []byte
		wantData string
	}{
		{name: "default collects and filters incoming request body", bodies: nil, body: []byte(`{"key":"value","safe":"value"}`), wantData: `{"key":"[Filtered]","safe":"value"}`},
		{name: "empty disables request body", bodies: []BodyType{}, body: []byte(`{"key":"value"}`), wantData: ""},
		{name: "incoming request enabled", bodies: []BodyType{BodyIncomingRequest}, body: []byte(`{"safe":"value"}`), wantData: `{"safe":"value"}`},
		{name: "other body type does not collect request body", bodies: []BodyType{BodyOutgoingRequest}, body: []byte(`{"key":"value"}`), wantData: ""},
		{name: "opaque body is filtered entirely", bodies: nil, body: []byte(`raw body`), wantData: filteredValue},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(ClientOptions{
				Dsn:            "https://key@sentry.io/1",
				DataCollection: &DataCollection{HTTPBodies: tt.bodies},
			})
			if err != nil {
				t.Fatal(err)
			}

			scope := NewScope()
			request := httptest.NewRequest("POST", "http://example.com/test", nil)
			scope.SetRequest(request)
			scope.SetRequestBody(tt.body)

			event := scope.ApplyToEvent(NewEvent(), nil, client)
			if event.Request == nil {
				t.Fatal("expected request to be attached")
			}
			if diff := cmp.Diff(tt.wantData, event.Request.Data); diff != "" {
				t.Errorf("request body mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
