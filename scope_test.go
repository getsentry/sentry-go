package sentry

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const sharedContextsKey = "sharedContextsKey"

var testNow = time.Now().UTC()

func fillScopeWithData(scope *Scope) *Scope {
	scope.breadcrumbs = []*Breadcrumb{{Timestamp: testNow, Message: "scopeBreadcrumbMessage"}}
	scope.user = User{ID: "1337"}
	scope.tags = map[string]string{"scopeTagKey": "scopeTagValue"}
	scope.contexts = map[string]Context{
		"scopeContextsKey": {"scopeContextKey": "scopeContextValue"},
		sharedContextsKey:  {"scopeContextKey": "scopeContextValue"},
	}
	scope.extra = map[string]interface{}{"scopeExtraKey": "scopeExtraValue"}
	scope.fingerprint = []string{"scopeFingerprintOne", "scopeFingerprintTwo"}
	scope.level = LevelDebug
	scope.transaction = "wat"
	scope.request = httptest.NewRequest("GET", "/wat", nil)
	return scope
}

func fillEventWithData(event *Event) *Event {
	event.Breadcrumbs = []*Breadcrumb{{Timestamp: testNow, Message: "eventBreadcrumbMessage"}}
	event.User = User{ID: "42"}
	event.Tags = map[string]string{"eventTagKey": "eventTagValue"}
	event.Contexts = map[string]Context{
		"eventContextsKey": {"eventContextKey": "eventContextValue"},
		sharedContextsKey:  {"eventContextKey": "eventContextKey"},
	}
	event.Extra = map[string]interface{}{"eventExtraKey": "eventExtraValue"}
	event.Fingerprint = []string{"eventFingerprintOne", "eventFingerprintTwo"}
	event.Level = LevelInfo
	event.Transaction = "aye"
	event.Request = &Request{URL: "aye"}
	return event
}

func TestScopeSetUser(t *testing.T) {
	scope := NewScope()
	scope.SetUser(User{ID: "foo", Email: "foo@example.com", IPAddress: "127.0.0.1", Username: "My Username", Name: "My Name", Segment: "My Segment", Data: map[string]string{"foo": "bar"}})

	assertEqual(t, User{ID: "foo", Email: "foo@example.com", IPAddress: "127.0.0.1", Username: "My Username", Name: "My Name", Segment: "My Segment", Data: map[string]string{"foo": "bar"}}, scope.user)
}

func TestScopeSetUserOverrides(t *testing.T) {
	scope := NewScope()
	scope.SetUser(User{ID: "foo"})
	scope.SetUser(User{ID: "bar"})

	assertEqual(t, User{ID: "bar"}, scope.user)
}

func TestScopeSetRequest(t *testing.T) {
	r := httptest.NewRequest("GET", "/foo", nil)
	scope := NewScope()
	scope.SetRequest(r)

	assertEqual(t, r, scope.request)
}

func TestScopeSetRequestOverrides(t *testing.T) {
	r1 := httptest.NewRequest("GET", "/foo", nil)
	r2 := httptest.NewRequest("GET", "/bar", nil)
	scope := NewScope()
	scope.SetRequest(r1)
	scope.SetRequest(r2)

	assertEqual(t, r2, scope.request)
}

func TestScopeSetTag(t *testing.T) {
	scope := NewScope()
	scope.SetTag("a", "foo")

	assertEqual(t, map[string]string{"a": "foo"}, scope.tags)
}

func TestScopeSetTagMerges(t *testing.T) {
	scope := NewScope()
	scope.SetTag("a", "foo")
	scope.SetTag("b", "bar")

	assertEqual(t, map[string]string{"a": "foo", "b": "bar"}, scope.tags)
}

func TestScopeSetTagOverrides(t *testing.T) {
	scope := NewScope()
	scope.SetTag("a", "foo")
	scope.SetTag("a", "bar")

	assertEqual(t, map[string]string{"a": "bar"}, scope.tags)
}

func TestScopeSetTags(t *testing.T) {
	scope := NewScope()
	scope.SetTags(map[string]string{"a": "foo"})

	assertEqual(t, map[string]string{"a": "foo"}, scope.tags)
}

func TestScopeSetTagsMerges(t *testing.T) {
	scope := NewScope()
	scope.SetTags(map[string]string{"a": "foo"})
	scope.SetTags(map[string]string{"b": "bar", "c": "baz"})

	assertEqual(t, map[string]string{"a": "foo", "b": "bar", "c": "baz"}, scope.tags)
}

func TestScopeSetTagsOverrides(t *testing.T) {
	scope := NewScope()
	scope.SetTags(map[string]string{"a": "foo"})
	scope.SetTags(map[string]string{"a": "bar", "b": "baz"})

	assertEqual(t, map[string]string{"a": "bar", "b": "baz"}, scope.tags)
}

func TestScopeRemoveTag(t *testing.T) {
	scope := NewScope()
	scope.SetTag("a", "foo")
	scope.SetTag("b", "bar")
	scope.RemoveTag("b")

	assertEqual(t, map[string]string{"a": "foo"}, scope.tags)
}

func TestScopeRemoveTagSkipsEmptyValues(t *testing.T) {
	scope := NewScope()
	scope.SetTag("a", "foo")
	scope.RemoveTag("b")

	assertEqual(t, map[string]string{"a": "foo"}, scope.tags)
}

func TestScopeRemoveTagOnEmptyScope(t *testing.T) {
	scope := NewScope()
	scope.RemoveTag("b")

	assertEqual(t, make(map[string]string), scope.tags)
}

func TestScopeSetContext(t *testing.T) {
	scope := NewScope()
	scope.SetContext("a", Context{"b": 1})

	assertEqual(t, map[string]Context{"a": {"b": 1}}, scope.contexts)
}

func TestScopeSetContextMerges(t *testing.T) {
	scope := NewScope()
	scope.SetContext("a", Context{"foo": "bar"})
	scope.SetContext("b", Context{"b": 2})

	assertEqual(t, map[string]Context{"a": {"foo": "bar"}, "b": {"b": 2}}, scope.contexts)
}

func TestScopeSetContextOverrides(t *testing.T) {
	scope := NewScope()
	scope.SetContext("a", Context{"foo": "bar"})
	scope.SetContext("a", Context{"foo": 2})

	assertEqual(t, map[string]Context{"a": {"foo": 2}}, scope.contexts)
}

func TestScopeSetContexts(t *testing.T) {
	scope := NewScope()
	scope.SetContexts(map[string]Context{"a": {"b": 1}})

	assertEqual(t, map[string]Context{"a": {"b": 1}}, scope.contexts)
}

func TestScopeSetContextsMerges(t *testing.T) {
	scope := NewScope()
	scope.SetContexts(map[string]Context{"a": {"a": "foo"}})
	scope.SetContexts(map[string]Context{"b": {"b": 2}, "c": {"c": 3}})

	assertEqual(t, map[string]Context{"a": {"a": "foo"}, "b": {"b": 2}, "c": {"c": 3}}, scope.contexts)
}

func TestScopeSetContextsOverrides(t *testing.T) {
	scope := NewScope()
	scope.SetContexts(map[string]Context{"a": {"foo": "bar"}})
	scope.SetContexts(map[string]Context{"a": {"a": 2}, "b": {"b": 3}})

	assertEqual(t, map[string]Context{"a": {"a": 2}, "b": {"b": 3}}, scope.contexts)
}

func TestScopeRemoveContext(t *testing.T) {
	scope := NewScope()
	scope.SetContext("a", Context{"foo": "foo"})
	scope.SetContext("b", Context{"bar": "bar"})
	scope.RemoveContext("b")

	assertEqual(t, map[string]Context{"a": {"foo": "foo"}}, scope.contexts)
}

func TestScopeRemoveContextSkipsEmptyValues(t *testing.T) {
	scope := NewScope()
	scope.SetContext("a", Context{"foo": "bar"})
	scope.RemoveContext("b")

	assertEqual(t, map[string]Context{"a": {"foo": "bar"}}, scope.contexts)
}

func TestScopeRemoveContextOnEmptyScope(t *testing.T) {
	scope := NewScope()
	scope.RemoveContext("b")

	assertEqual(t, make(map[string]Context), scope.contexts)
}

func TestScopeSetExtra(t *testing.T) {
	scope := NewScope()
	scope.SetExtra("a", 1)

	assertEqual(t, map[string]interface{}{"a": 1}, scope.extra)
}

func TestScopeSetExtraMerges(t *testing.T) {
	scope := NewScope()
	scope.SetExtra("a", "foo")
	scope.SetExtra("b", 2)

	assertEqual(t, map[string]interface{}{"a": "foo", "b": 2}, scope.extra)
}

func TestScopeSetExtraOverrides(t *testing.T) {
	scope := NewScope()
	scope.SetExtra("a", "foo")
	scope.SetExtra("a", 2)

	assertEqual(t, map[string]interface{}{"a": 2}, scope.extra)
}

func TestScopeSetExtras(t *testing.T) {
	scope := NewScope()
	scope.SetExtras(map[string]interface{}{"a": 1})

	assertEqual(t, map[string]interface{}{"a": 1}, scope.extra)
}

func TestScopeSetExtrasMerges(t *testing.T) {
	scope := NewScope()
	scope.SetExtras(map[string]interface{}{"a": "foo"})
	scope.SetExtras(map[string]interface{}{"b": 2, "c": 3})

	assertEqual(t, map[string]interface{}{"a": "foo", "b": 2, "c": 3}, scope.extra)
}

func TestScopeSetExtrasOverrides(t *testing.T) {
	scope := NewScope()
	scope.SetExtras(map[string]interface{}{"a": "foo"})
	scope.SetExtras(map[string]interface{}{"a": 2, "b": 3})

	assertEqual(t, map[string]interface{}{"a": 2, "b": 3}, scope.extra)
}

func TestScopeRemoveExtra(t *testing.T) {
	scope := NewScope()
	scope.SetExtra("a", "foo")
	scope.SetExtra("b", "bar")
	scope.RemoveExtra("b")

	assertEqual(t, map[string]interface{}{"a": "foo"}, scope.extra)
}

func TestScopeRemoveExtraSkipsEmptyValues(t *testing.T) {
	scope := NewScope()
	scope.SetExtra("a", "foo")
	scope.RemoveExtra("b")

	assertEqual(t, map[string]interface{}{"a": "foo"}, scope.extra)
}

func TestScopeRemoveExtraOnEmptyScope(t *testing.T) {
	scope := NewScope()
	scope.RemoveExtra("b")

	assertEqual(t, make(map[string]Context), scope.contexts)
}

func TestScopeSetFingerprint(t *testing.T) {
	scope := NewScope()
	scope.SetFingerprint([]string{"abcd"})

	assertEqual(t, []string{"abcd"}, scope.fingerprint)
}

func TestScopeSetFingerprintOverrides(t *testing.T) {
	scope := NewScope()
	scope.SetFingerprint([]string{"abc"})
	scope.SetFingerprint([]string{"def"})

	assertEqual(t, []string{"def"}, scope.fingerprint)
}

func TestScopeSetLevel(t *testing.T) {
	scope := NewScope()
	scope.SetLevel(LevelInfo)

	assertEqual(t, scope.level, LevelInfo)
}

func TestScopeSetLevelOverrides(t *testing.T) {
	scope := NewScope()
	scope.SetLevel(LevelInfo)
	scope.SetLevel(LevelFatal)

	assertEqual(t, scope.level, LevelFatal)
}

func TestScopeSetTransaction(t *testing.T) {
	scope := NewScope()
	scope.SetTransaction("abc")

	assertEqual(t, scope.transaction, "abc")
}

func TestScopeSetTransactionOverrides(t *testing.T) {
	scope := NewScope()
	scope.SetTransaction("abc")
	scope.SetTransaction("def")

	assertEqual(t, scope.transaction, "def")
}

func TestAddBreadcrumbAddsBreadcrumb(t *testing.T) {
	scope := NewScope()
	scope.AddBreadcrumb(&Breadcrumb{Timestamp: testNow, Message: "test"}, maxBreadcrumbs)
	assertEqual(t, []*Breadcrumb{{Timestamp: testNow, Message: "test"}}, scope.breadcrumbs)
}

func TestAddBreadcrumbAppendsBreadcrumb(t *testing.T) {
	scope := NewScope()
	scope.AddBreadcrumb(&Breadcrumb{Timestamp: testNow, Message: "test1"}, maxBreadcrumbs)
	scope.AddBreadcrumb(&Breadcrumb{Timestamp: testNow, Message: "test2"}, maxBreadcrumbs)
	scope.AddBreadcrumb(&Breadcrumb{Timestamp: testNow, Message: "test3"}, maxBreadcrumbs)

	assertEqual(t, []*Breadcrumb{
		{Timestamp: testNow, Message: "test1"},
		{Timestamp: testNow, Message: "test2"},
		{Timestamp: testNow, Message: "test3"},
	}, scope.breadcrumbs)
}

func TestAddBreadcrumbDefaultLimit(t *testing.T) {
	scope := NewScope()
	for i := 0; i < 101; i++ {
		scope.AddBreadcrumb(&Breadcrumb{Timestamp: testNow, Message: "test"}, maxBreadcrumbs)
	}

	if len(scope.breadcrumbs) != 100 {
		t.Error("expected to have only 100 breadcrumbs")
	}
}

func TestAddBreadcrumbAddsTimestamp(t *testing.T) {
	scope := NewScope()
	before := time.Now()
	scope.AddBreadcrumb(&Breadcrumb{Message: "test"}, maxBreadcrumbs)
	after := time.Now()
	ts := scope.breadcrumbs[0].Timestamp

	if ts.Before(before) || ts.After(after) {
		t.Errorf("expected default timestamp to represent current time, was '%v'", ts)
	}
}

func TestScopeBasicInheritance(t *testing.T) {
	scope := NewScope()
	scope.SetExtra("a", 1)
	scope.SetRequestBody([]byte("requestbody"))
	scope.AddEventProcessor(func(event *Event, hint *EventHint) *Event {
		return event
	})
	clone := scope.Clone()

	assertEqual(t, scope.extra, clone.extra)
	assertEqual(t, scope.requestBody, clone.requestBody)
	assertEqual(t, scope.eventProcessors, clone.eventProcessors)
}

func TestScopeParentChangedInheritance(t *testing.T) {
	scope := NewScope()
	clone := scope.Clone()

	clone.SetTag("foo", "bar")
	clone.SetContext("foo", Context{"foo": "bar"})
	clone.SetExtra("foo", "bar")
	clone.SetLevel(LevelDebug)
	clone.SetTransaction("foo")
	clone.SetFingerprint([]string{"foo"})
	clone.AddBreadcrumb(&Breadcrumb{Timestamp: testNow, Message: "foo"}, maxBreadcrumbs)
	clone.SetUser(User{ID: "foo"})
	r1 := httptest.NewRequest("GET", "/foo", nil)
	clone.SetRequest(r1)

	scope.SetTag("foo", "baz")
	scope.SetContext("foo", Context{"foo": "baz"})
	scope.SetExtra("foo", "baz")
	scope.SetLevel(LevelFatal)
	scope.SetTransaction("bar")
	scope.SetFingerprint([]string{"bar"})
	scope.AddBreadcrumb(&Breadcrumb{Timestamp: testNow, Message: "bar"}, maxBreadcrumbs)
	scope.SetUser(User{ID: "bar"})
	r2 := httptest.NewRequest("GET", "/bar", nil)
	scope.SetRequest(r2)

	assertEqual(t, map[string]string{"foo": "bar"}, clone.tags)
	assertEqual(t, map[string]Context{"foo": {"foo": "bar"}}, clone.contexts)
	assertEqual(t, map[string]interface{}{"foo": "bar"}, clone.extra)
	assertEqual(t, LevelDebug, clone.level)
	assertEqual(t, "foo", clone.transaction)
	assertEqual(t, []string{"foo"}, clone.fingerprint)
	assertEqual(t, []*Breadcrumb{{Timestamp: testNow, Message: "foo"}}, clone.breadcrumbs)
	assertEqual(t, User{ID: "foo"}, clone.user)
	assertEqual(t, r1, clone.request)

	assertEqual(t, map[string]string{"foo": "baz"}, scope.tags)
	assertEqual(t, map[string]Context{"foo": {"foo": "baz"}}, scope.contexts)
	assertEqual(t, map[string]interface{}{"foo": "baz"}, scope.extra)
	assertEqual(t, LevelFatal, scope.level)
	assertEqual(t, "bar", scope.transaction)
	assertEqual(t, []string{"bar"}, scope.fingerprint)
	assertEqual(t, []*Breadcrumb{{Timestamp: testNow, Message: "bar"}}, scope.breadcrumbs)
	assertEqual(t, User{ID: "bar"}, scope.user)
	assertEqual(t, r2, scope.request)
}

func TestScopeChildOverrideInheritance(t *testing.T) {
	scope := NewScope()

	scope.SetTag("foo", "baz")
	scope.SetContext("foo", Context{"foo": "baz"})
	scope.SetExtra("foo", "baz")
	scope.SetLevel(LevelFatal)
	scope.SetTransaction("bar")
	scope.SetFingerprint([]string{"bar"})
	scope.AddBreadcrumb(&Breadcrumb{Timestamp: testNow, Message: "bar"}, maxBreadcrumbs)
	scope.SetUser(User{ID: "bar"})
	r1 := httptest.NewRequest("GET", "/bar", nil)
	scope.SetRequest(r1)
	scope.AddEventProcessor(func(event *Event, hint *EventHint) *Event {
		return event
	})

	clone := scope.Clone()
	clone.SetTag("foo", "bar")
	clone.SetContext("foo", Context{"foo": "bar"})
	clone.SetExtra("foo", "bar")
	clone.SetLevel(LevelDebug)
	clone.SetTransaction("foo")
	clone.SetFingerprint([]string{"foo"})
	clone.AddBreadcrumb(&Breadcrumb{Timestamp: testNow, Message: "foo"}, maxBreadcrumbs)
	clone.SetUser(User{ID: "foo"})
	r2 := httptest.NewRequest("GET", "/foo", nil)
	clone.SetRequest(r2)
	clone.AddEventProcessor(func(event *Event, hint *EventHint) *Event {
		return event
	})

	assertEqual(t, map[string]string{"foo": "bar"}, clone.tags)
	assertEqual(t, map[string]Context{"foo": {"foo": "bar"}}, clone.contexts)
	assertEqual(t, map[string]interface{}{"foo": "bar"}, clone.extra)
	assertEqual(t, LevelDebug, clone.level)
	assertEqual(t, "foo", clone.transaction)
	assertEqual(t, []string{"foo"}, clone.fingerprint)
	assertEqual(t, []*Breadcrumb{
		{Timestamp: testNow, Message: "bar"},
		{Timestamp: testNow, Message: "foo"},
	}, clone.breadcrumbs)
	assertEqual(t, User{ID: "foo"}, clone.user)
	assertEqual(t, r2, clone.request)

	assertEqual(t, map[string]string{"foo": "baz"}, scope.tags)
	assertEqual(t, map[string]Context{"foo": {"foo": "baz"}}, scope.contexts)
	assertEqual(t, map[string]interface{}{"foo": "baz"}, scope.extra)
	assertEqual(t, LevelFatal, scope.level)
	assertEqual(t, "bar", scope.transaction)
	assertEqual(t, []string{"bar"}, scope.fingerprint)
	assertEqual(t, []*Breadcrumb{{Timestamp: testNow, Message: "bar"}}, scope.breadcrumbs)
	assertEqual(t, User{ID: "bar"}, scope.user)
	assertEqual(t, r1, scope.request)

	assertEqual(t, len(scope.eventProcessors), 1)
	assertEqual(t, len(clone.eventProcessors), 2)
}

func TestClear(t *testing.T) {
	scope := fillScopeWithData(NewScope())
	scope.Clear()

	assertEqual(t, []*Breadcrumb{}, scope.breadcrumbs)
	assertEqual(t, User{}, scope.user)
	assertEqual(t, map[string]string{}, scope.tags)
	assertEqual(t, map[string]Context{}, scope.contexts)
	assertEqual(t, map[string]interface{}{}, scope.extra)
	assertEqual(t, []string{}, scope.fingerprint)
	assertEqual(t, Level(""), scope.level)
	assertEqual(t, "", scope.transaction)
	assertEqual(t, (*http.Request)(nil), scope.request)
}

func TestClearAndReconfigure(t *testing.T) {
	scope := fillScopeWithData(NewScope())
	scope.Clear()

	scope.SetTag("foo", "bar")
	scope.SetContext("foo", Context{"foo": "bar"})
	scope.SetExtra("foo", "bar")
	scope.SetLevel(LevelDebug)
	scope.SetTransaction("foo")
	scope.SetFingerprint([]string{"foo"})
	scope.AddBreadcrumb(&Breadcrumb{Timestamp: testNow, Message: "foo"}, maxBreadcrumbs)
	scope.SetUser(User{ID: "foo"})
	r := httptest.NewRequest("GET", "/foo", nil)
	scope.SetRequest(r)

	assertEqual(t, map[string]string{"foo": "bar"}, scope.tags)
	assertEqual(t, map[string]Context{"foo": {"foo": "bar"}}, scope.contexts)
	assertEqual(t, map[string]interface{}{"foo": "bar"}, scope.extra)
	assertEqual(t, LevelDebug, scope.level)
	assertEqual(t, "foo", scope.transaction)
	assertEqual(t, []string{"foo"}, scope.fingerprint)
	assertEqual(t, []*Breadcrumb{{Timestamp: testNow, Message: "foo"}}, scope.breadcrumbs)
	assertEqual(t, User{ID: "foo"}, scope.user)
	assertEqual(t, r, scope.request)
}

func TestClearBreadcrumbs(t *testing.T) {
	scope := fillScopeWithData(NewScope())
	scope.ClearBreadcrumbs()

	assertEqual(t, []*Breadcrumb{}, scope.breadcrumbs)
}

func TestApplyToEventWithCorrectScopeAndEvent(t *testing.T) {
	scope := fillScopeWithData(NewScope())
	event := fillEventWithData(NewEvent())

	processedEvent := scope.ApplyToEvent(event, nil)

	assertEqual(t, len(processedEvent.Breadcrumbs), 2, "should merge breadcrumbs")
	assertEqual(t, len(processedEvent.Tags), 2, "should merge tags")
	assertEqual(t, len(processedEvent.Contexts), 3, "should merge contexts")
	assertEqual(t, event.Contexts[sharedContextsKey], event.Contexts[sharedContextsKey], "should not override event context")
	assertEqual(t, len(processedEvent.Extra), 2, "should merge extra")
	assertEqual(t, processedEvent.Level, scope.level, "should use scope level if its set")
	assertEqual(t, processedEvent.Transaction, scope.transaction, "should use scope transaction if its set")
	assertNotEqual(t, processedEvent.User, scope.user, "should use event user if one exist")
	assertNotEqual(t, processedEvent.Request, scope.request, "should use event request if one exist")
	assertNotEqual(t, processedEvent.Fingerprint, scope.fingerprint, "should use event fingerprints if they exist")
}

func TestApplyToEventUsingEmptyScope(t *testing.T) {
	scope := NewScope()
	event := fillEventWithData(NewEvent())

	processedEvent := scope.ApplyToEvent(event, nil)

	assertEqual(t, len(processedEvent.Breadcrumbs), 1, "should use event breadcrumbs")
	assertEqual(t, len(processedEvent.Tags), 1, "should use event tags")
	assertEqual(t, len(processedEvent.Contexts), 2, "should use event contexts")
	assertEqual(t, len(processedEvent.Extra), 1, "should use event extra")
	assertNotEqual(t, processedEvent.User, scope.user, "should use event user")
	assertNotEqual(t, processedEvent.Fingerprint, scope.fingerprint, "should use event fingerprint")
	assertNotEqual(t, processedEvent.Level, scope.level, "should use event level")
	assertNotEqual(t, processedEvent.Transaction, scope.transaction, "should use event transaction")
	assertNotEqual(t, processedEvent.Request, scope.request, "should use event request")
}

func TestApplyToEventUsingEmptyEvent(t *testing.T) {
	scope := fillScopeWithData(NewScope())
	event := NewEvent()

	processedEvent := scope.ApplyToEvent(event, nil)

	assertEqual(t, len(processedEvent.Breadcrumbs), 1, "should use scope breadcrumbs")
	assertEqual(t, len(processedEvent.Tags), 1, "should use scope tags")
	assertEqual(t, len(processedEvent.Contexts), 2, "should use scope contexts")
	assertEqual(t, len(processedEvent.Extra), 1, "should use scope extra")
	assertEqual(t, processedEvent.User, scope.user, "should use scope user")
	assertEqual(t, processedEvent.Fingerprint, scope.fingerprint, "should use scope fingerprint")
	assertEqual(t, processedEvent.Level, scope.level, "should use scope level")
	assertEqual(t, processedEvent.Transaction, scope.transaction, "should use scope transaction")
	assertEqual(t, processedEvent.Request, NewRequest(scope.request), "should use scope request")
}

func TestEventProcessorsModifiesEvent(t *testing.T) {
	scope := NewScope()
	event := NewEvent()
	scope.eventProcessors = []EventProcessor{
		func(event *Event, hint *EventHint) *Event {
			event.Level = LevelFatal
			return event
		},
		func(event *Event, hint *EventHint) *Event {
			event.Fingerprint = []string{"wat"}
			return event
		},
	}
	processedEvent := scope.ApplyToEvent(event, nil)

	if processedEvent == nil {
		t.Fatal("event should not be dropped")
	}
	assertEqual(t, LevelFatal, processedEvent.Level)
	assertEqual(t, []string{"wat"}, processedEvent.Fingerprint)
}

func TestEventProcessorsCanDropEvent(t *testing.T) {
	scope := NewScope()
	event := NewEvent()
	scope.eventProcessors = []EventProcessor{
		func(event *Event, hint *EventHint) *Event {
			return nil
		},
	}
	processedEvent := scope.ApplyToEvent(event, nil)

	if processedEvent != nil {
		t.Error("event should be dropped")
	}
}

func TestEventProcessorsAddEventProcessor(t *testing.T) {
	scope := NewScope()
	event := NewEvent()
	processedEvent := scope.ApplyToEvent(event, nil)

	if processedEvent == nil {
		t.Error("event should not be dropped")
	}

	scope.AddEventProcessor(func(event *Event, hint *EventHint) *Event {
		return nil
	})
	processedEvent = scope.ApplyToEvent(event, nil)

	if processedEvent != nil {
		t.Error("event should be dropped")
	}
}
