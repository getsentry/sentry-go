package sentry

import (
	"testing"
	"time"
)

func fillScopeWithData(scope *Scope) *Scope {
	scope.breadcrumbs = []*Breadcrumb{{Timestamp: 1337, Message: "scopeBreadcrumbMessage"}}
	scope.user = User{ID: "1337"}
	scope.contexts = map[string]interface{}{"scopeContextsKey": "scopeContextsValue"}
	scope.tags = map[string]string{"scopeTagKey": "scopeTagValue"}
	scope.extra = map[string]interface{}{"scopeExtraKey": "scopeExtraValue"}
	scope.fingerprint = []string{"scopeFingerprintOne", "scopeFingerprintTwo"}
	scope.level = LevelDebug
	return scope
}

func fillEventWithData(event *Event) *Event {
	event.Breadcrumbs = []*Breadcrumb{{Timestamp: 1337, Message: "eventBreadcrumbMessage"}}
	event.User = User{ID: "42"}
	event.Contexts = map[string]interface{}{"eventContextsKey": "eventContextsValue"}
	event.Tags = map[string]string{"eventTagKey": "eventTagValue"}
	event.Extra = map[string]interface{}{"eventExtraKey": "eventExtraValue"}
	event.Fingerprint = []string{"eventFingerprintOne", "eventFingerprintTwo"}
	event.Level = LevelInfo
	return event
}

func TestScopeSetUser(t *testing.T) {
	scope := &Scope{}
	scope.SetUser(User{ID: "foo"})
	assertEqual(t, User{ID: "foo"}, scope.user)
}

func TestScopeSetUserOverrides(t *testing.T) {
	scope := &Scope{}
	scope.SetUser(User{ID: "foo"})
	scope.SetUser(User{ID: "bar"})

	assertEqual(t, User{ID: "bar"}, scope.user)
}

func TestScopeSetTag(t *testing.T) {
	scope := &Scope{}
	scope.SetTag("a", "foo")

	assertEqual(t, map[string]string{"a": "foo"}, scope.tags)
}

func TestScopeSetTagMerges(t *testing.T) {
	scope := &Scope{}
	scope.SetTag("a", "foo")
	scope.SetTag("b", "bar")

	assertEqual(t, map[string]string{"a": "foo", "b": "bar"}, scope.tags)
}

func TestScopeSetTagOverrides(t *testing.T) {
	scope := &Scope{}
	scope.SetTag("a", "foo")
	scope.SetTag("a", "bar")

	assertEqual(t, map[string]string{"a": "bar"}, scope.tags)
}

func TestScopeSetTags(t *testing.T) {
	scope := &Scope{}
	scope.SetTags(map[string]string{"a": "foo"})

	assertEqual(t, map[string]string{"a": "foo"}, scope.tags)
}

func TestScopeSetTagsMerges(t *testing.T) {
	scope := &Scope{}
	scope.SetTags(map[string]string{"a": "foo"})
	scope.SetTags(map[string]string{"b": "bar", "c": "baz"})

	assertEqual(t, map[string]string{"a": "foo", "b": "bar", "c": "baz"}, scope.tags)
}

func TestScopeSetTagsOverrides(t *testing.T) {
	scope := &Scope{}
	scope.SetTags(map[string]string{"a": "foo"})
	scope.SetTags(map[string]string{"a": "bar", "b": "baz"})

	assertEqual(t, map[string]string{"a": "bar", "b": "baz"}, scope.tags)
}

func TestScopeRemoveTag(t *testing.T) {
	scope := &Scope{}
	scope.SetTag("a", "foo")
	scope.SetTag("b", "bar")
	scope.RemoveTag("b")

	assertEqual(t, map[string]string{"a": "foo"}, scope.tags)
}

func TestScopeRemoveTagSkipsEmptyValues(t *testing.T) {
	scope := &Scope{}
	scope.SetTag("a", "foo")
	scope.RemoveTag("b")

	assertEqual(t, map[string]string{"a": "foo"}, scope.tags)
}

func TestScopeRemoveTagOnEmptyScope(t *testing.T) {
	scope := &Scope{}
	scope.RemoveTag("b")

	assertEqual(t, map[string]string(nil), scope.tags)
}

func TestScopeSetContext(t *testing.T) {
	scope := &Scope{}
	scope.SetContext("a", 1)

	assertEqual(t, map[string]interface{}{"a": 1}, scope.contexts)
}

func TestScopeSetContextMerges(t *testing.T) {
	scope := &Scope{}
	scope.SetContext("a", "foo")
	scope.SetContext("b", 2)

	assertEqual(t, map[string]interface{}{"a": "foo", "b": 2}, scope.contexts)
}

func TestScopeSetContextOverrides(t *testing.T) {
	scope := &Scope{}
	scope.SetContext("a", "foo")
	scope.SetContext("a", 2)

	assertEqual(t, map[string]interface{}{"a": 2}, scope.contexts)
}

func TestScopeSetContexts(t *testing.T) {
	scope := &Scope{}
	scope.SetContexts(map[string]interface{}{"a": 1})

	assertEqual(t, map[string]interface{}{"a": 1}, scope.contexts)
}

func TestScopeSetContextsMerges(t *testing.T) {
	scope := &Scope{}
	scope.SetContexts(map[string]interface{}{"a": "foo"})
	scope.SetContexts(map[string]interface{}{"b": 2, "c": 3})

	assertEqual(t, map[string]interface{}{"a": "foo", "b": 2, "c": 3}, scope.contexts)
}

func TestScopeSetContextsOverrides(t *testing.T) {
	scope := &Scope{}
	scope.SetContexts(map[string]interface{}{"a": "foo"})
	scope.SetContexts(map[string]interface{}{"a": 2, "b": 3})

	assertEqual(t, map[string]interface{}{"a": 2, "b": 3}, scope.contexts)
}

func TestScopeRemoveContext(t *testing.T) {
	scope := &Scope{}
	scope.SetContext("a", "foo")
	scope.SetContext("b", "bar")
	scope.RemoveContext("b")

	assertEqual(t, map[string]interface{}{"a": "foo"}, scope.contexts)
}

func TestScopeRemoveContextSkipsEmptyValues(t *testing.T) {
	scope := &Scope{}
	scope.SetContext("a", "foo")
	scope.RemoveContext("b")

	assertEqual(t, map[string]interface{}{"a": "foo"}, scope.contexts)
}

func TestScopeRemoveContextOnEmptyScope(t *testing.T) {
	scope := &Scope{}
	scope.RemoveContext("b")

	assertEqual(t, map[string]interface{}(nil), scope.contexts)
}

func TestScopeSetExtra(t *testing.T) {
	scope := &Scope{}
	scope.SetExtra("a", 1)

	assertEqual(t, map[string]interface{}{"a": 1}, scope.extra)
}

func TestScopeSetExtraMerges(t *testing.T) {
	scope := &Scope{}
	scope.SetExtra("a", "foo")
	scope.SetExtra("b", 2)

	assertEqual(t, map[string]interface{}{"a": "foo", "b": 2}, scope.extra)
}

func TestScopeSetExtraOverrides(t *testing.T) {
	scope := &Scope{}
	scope.SetExtra("a", "foo")
	scope.SetExtra("a", 2)

	assertEqual(t, map[string]interface{}{"a": 2}, scope.extra)
}

func TestScopeSetExtras(t *testing.T) {
	scope := &Scope{}
	scope.SetExtras(map[string]interface{}{"a": 1})

	assertEqual(t, map[string]interface{}{"a": 1}, scope.extra)
}

func TestScopeSetExtrasMerges(t *testing.T) {
	scope := &Scope{}
	scope.SetExtras(map[string]interface{}{"a": "foo"})
	scope.SetExtras(map[string]interface{}{"b": 2, "c": 3})

	assertEqual(t, map[string]interface{}{"a": "foo", "b": 2, "c": 3}, scope.extra)
}

func TestScopeSetExtrasOverrides(t *testing.T) {
	scope := &Scope{}
	scope.SetExtras(map[string]interface{}{"a": "foo"})
	scope.SetExtras(map[string]interface{}{"a": 2, "b": 3})

	assertEqual(t, map[string]interface{}{"a": 2, "b": 3}, scope.extra)
}

func TestScopeRemoveExtra(t *testing.T) {
	scope := &Scope{}
	scope.SetExtra("a", "foo")
	scope.SetExtra("b", "bar")
	scope.RemoveExtra("b")

	assertEqual(t, map[string]interface{}{"a": "foo"}, scope.extra)
}

func TestScopeRemoveExtraSkipsEmptyValues(t *testing.T) {
	scope := &Scope{}
	scope.SetExtra("a", "foo")
	scope.RemoveExtra("b")

	assertEqual(t, map[string]interface{}{"a": "foo"}, scope.extra)
}

func TestScopeRemoveExtraOnEmptyScope(t *testing.T) {
	scope := &Scope{}
	scope.RemoveExtra("b")

	assertEqual(t, map[string]interface{}(nil), scope.contexts)
}

func TestScopeSetFingerprint(t *testing.T) {
	scope := &Scope{}
	scope.SetFingerprint([]string{"abcd"})

	assertEqual(t, []string{"abcd"}, scope.fingerprint)
}

func TestScopeSetFingerprintOverrides(t *testing.T) {
	scope := &Scope{}
	scope.SetFingerprint([]string{"abc"})
	scope.SetFingerprint([]string{"def"})

	assertEqual(t, []string{"def"}, scope.fingerprint)
}

func TestScopeSetLevel(t *testing.T) {
	scope := &Scope{}
	scope.SetLevel(LevelInfo)

	assertEqual(t, scope.level, LevelInfo)
}

func TestScopeSetLevelOverrides(t *testing.T) {
	scope := &Scope{}
	scope.SetLevel(LevelInfo)
	scope.SetLevel(LevelFatal)

	assertEqual(t, scope.level, LevelFatal)
}

func TestAddBreadcrumbAddsBreadcrumb(t *testing.T) {
	scope := &Scope{}
	scope.AddBreadcrumb(&Breadcrumb{Timestamp: 1337, Message: "test"}, maxBreadcrumbs)
	assertEqual(t, []*Breadcrumb{{Timestamp: 1337, Message: "test"}}, scope.breadcrumbs)
}

func TestAddBreadcrumbAppendsBreadcrumb(t *testing.T) {
	scope := &Scope{}
	scope.AddBreadcrumb(&Breadcrumb{Timestamp: 1337, Message: "test1"}, maxBreadcrumbs)
	scope.AddBreadcrumb(&Breadcrumb{Timestamp: 1337, Message: "test2"}, maxBreadcrumbs)
	scope.AddBreadcrumb(&Breadcrumb{Timestamp: 1337, Message: "test3"}, maxBreadcrumbs)

	assertEqual(t, []*Breadcrumb{
		{Timestamp: 1337, Message: "test1"},
		{Timestamp: 1337, Message: "test2"},
		{Timestamp: 1337, Message: "test3"},
	}, scope.breadcrumbs)
}

func TestAddBreadcrumbDefaultLimit(t *testing.T) {
	scope := &Scope{}
	for i := 0; i < 101; i++ {
		scope.AddBreadcrumb(&Breadcrumb{Timestamp: 1337, Message: "test"}, maxBreadcrumbs)
	}

	if len(scope.breadcrumbs) != 100 {
		t.Error("expected to have only 100 breadcrumbs")
	}
}

func TestAddBreadcrumbAddsTimestamp(t *testing.T) {
	scope := &Scope{}
	before := time.Now().Unix()
	scope.AddBreadcrumb(&Breadcrumb{Message: "test"}, maxBreadcrumbs)
	after := time.Now().Unix()
	ts := scope.breadcrumbs[0].Timestamp

	if ts < before || ts > after {
		t.Errorf("expected default timestamp to represent current time, was '%d'", ts)
	}
}

func TestScopeBasicInheritance(t *testing.T) {
	scope := &Scope{}
	scope.SetExtra("a", 1)
	clone := scope.Clone()

	assertEqual(t, scope.extra, clone.extra)
}

func TestScopeParentChangedInheritance(t *testing.T) {
	scope := &Scope{}
	clone := scope.Clone()

	clone.SetTag("foo", "bar")
	clone.SetContext("foo", "bar")
	clone.SetExtra("foo", "bar")
	clone.SetLevel(LevelDebug)
	clone.SetFingerprint([]string{"foo"})
	clone.AddBreadcrumb(&Breadcrumb{Timestamp: 1337, Message: "foo"}, maxBreadcrumbs)
	clone.SetUser(User{ID: "foo"})

	scope.SetTag("foo", "baz")
	scope.SetContext("foo", "baz")
	scope.SetExtra("foo", "baz")
	scope.SetLevel(LevelFatal)
	scope.SetFingerprint([]string{"bar"})
	scope.AddBreadcrumb(&Breadcrumb{Timestamp: 1337, Message: "bar"}, maxBreadcrumbs)
	scope.SetUser(User{ID: "bar"})

	assertEqual(t, map[string]string{"foo": "bar"}, clone.tags)
	assertEqual(t, map[string]interface{}{"foo": "bar"}, clone.contexts)
	assertEqual(t, map[string]interface{}{"foo": "bar"}, clone.extra)
	assertEqual(t, LevelDebug, clone.level)
	assertEqual(t, []string{"foo"}, clone.fingerprint)
	assertEqual(t, []*Breadcrumb{{Timestamp: 1337, Message: "foo"}}, clone.breadcrumbs)
	assertEqual(t, User{ID: "foo"}, clone.user)

	assertEqual(t, map[string]string{"foo": "baz"}, scope.tags)
	assertEqual(t, map[string]interface{}{"foo": "baz"}, scope.contexts)
	assertEqual(t, map[string]interface{}{"foo": "baz"}, scope.extra)
	assertEqual(t, LevelFatal, scope.level)
	assertEqual(t, []string{"bar"}, scope.fingerprint)
	assertEqual(t, []*Breadcrumb{{Timestamp: 1337, Message: "bar"}}, scope.breadcrumbs)
	assertEqual(t, User{ID: "bar"}, scope.user)
}

func TestScopeChildOverrideInheritance(t *testing.T) {
	scope := &Scope{}

	scope.SetTag("foo", "baz")
	scope.SetContext("foo", "baz")
	scope.SetExtra("foo", "baz")
	scope.SetLevel(LevelFatal)
	scope.SetFingerprint([]string{"bar"})
	scope.AddBreadcrumb(&Breadcrumb{Timestamp: 1337, Message: "bar"}, maxBreadcrumbs)
	scope.SetUser(User{ID: "bar"})

	clone := scope.Clone()
	clone.SetTag("foo", "bar")
	clone.SetContext("foo", "bar")
	clone.SetExtra("foo", "bar")
	clone.SetLevel(LevelDebug)
	clone.SetFingerprint([]string{"foo"})
	clone.AddBreadcrumb(&Breadcrumb{Timestamp: 1337, Message: "foo"}, maxBreadcrumbs)
	clone.SetUser(User{ID: "foo"})

	assertEqual(t, map[string]string{"foo": "bar"}, clone.tags)
	assertEqual(t, map[string]interface{}{"foo": "bar"}, clone.contexts)
	assertEqual(t, map[string]interface{}{"foo": "bar"}, clone.extra)
	assertEqual(t, LevelDebug, clone.level)
	assertEqual(t, []string{"foo"}, clone.fingerprint)
	assertEqual(t, []*Breadcrumb{
		{Timestamp: 1337, Message: "bar"},
		{Timestamp: 1337, Message: "foo"},
	}, clone.breadcrumbs)
	assertEqual(t, User{ID: "foo"}, clone.user)

	assertEqual(t, map[string]string{"foo": "baz"}, scope.tags)
	assertEqual(t, map[string]interface{}{"foo": "baz"}, scope.contexts)
	assertEqual(t, map[string]interface{}{"foo": "baz"}, scope.extra)
	assertEqual(t, LevelFatal, scope.level)
	assertEqual(t, []string{"bar"}, scope.fingerprint)
	assertEqual(t, []*Breadcrumb{{Timestamp: 1337, Message: "bar"}}, scope.breadcrumbs)
	assertEqual(t, User{ID: "bar"}, scope.user)
}

func TestClear(t *testing.T) {
	scope := fillScopeWithData(&Scope{})
	scope.Clear()

	assertEqual(t, []*Breadcrumb(nil), scope.breadcrumbs)
	assertEqual(t, User{}, scope.user)
	assertEqual(t, map[string]string(nil), scope.tags)
	assertEqual(t, map[string]interface{}(nil), scope.contexts)
	assertEqual(t, map[string]interface{}(nil), scope.extra)
	assertEqual(t, []string(nil), scope.fingerprint)
	assertEqual(t, Level(""), scope.level)
}

func TestClearBreadcrumbs(t *testing.T) {
	scope := fillScopeWithData(&Scope{})
	scope.ClearBreadcrumbs()

	assertEqual(t, []*Breadcrumb{}, scope.breadcrumbs)
}

func TestApplyToEventWithCorrectScopeAndEvent(t *testing.T) {
	scope := fillScopeWithData(&Scope{})
	event := fillEventWithData(&Event{})

	processedEvent := scope.ApplyToEvent(event, nil)

	assertEqual(t, len(processedEvent.Breadcrumbs), 2, "should merge breadcrumbs")
	assertEqual(t, len(processedEvent.Tags), 2, "should merge tags")
	assertEqual(t, len(processedEvent.Contexts), 2, "should merge contexts")
	assertEqual(t, len(processedEvent.Extra), 2, "should merge extra")
	assertEqual(t, processedEvent.Level, scope.level, "should use scope level if its set")
	assertNotEqual(t, processedEvent.User, scope.user, "should use event user if one exist")
	assertNotEqual(t, processedEvent.Fingerprint, scope.fingerprint, "should use event fingerprints if they exist")
}

func TestApplyToEventUsingEmptyScope(t *testing.T) {
	scope := &Scope{}
	event := fillEventWithData(&Event{})

	processedEvent := scope.ApplyToEvent(event, nil)

	assertEqual(t, len(processedEvent.Breadcrumbs), 1, "should use event breadcrumbs")
	assertEqual(t, len(processedEvent.Tags), 1, "should use event tags")
	assertEqual(t, len(processedEvent.Contexts), 1, "should use event contexts")
	assertEqual(t, len(processedEvent.Extra), 1, "should use event extra")
	assertNotEqual(t, processedEvent.User, scope.user, "should use event user")
	assertNotEqual(t, processedEvent.Fingerprint, scope.fingerprint, "should use event fingerprint")
	assertNotEqual(t, processedEvent.Level, scope.level, "should use event level")
}

func TestApplyToEventUsingEmptyEvent(t *testing.T) {
	scope := fillScopeWithData(&Scope{})
	event := &Event{}

	processedEvent := scope.ApplyToEvent(event, nil)

	assertEqual(t, len(processedEvent.Breadcrumbs), 1, "should use scope breadcrumbs")
	assertEqual(t, len(processedEvent.Tags), 1, "should use scope tags")
	assertEqual(t, len(processedEvent.Contexts), 1, "should use scope contexts")
	assertEqual(t, len(processedEvent.Extra), 1, "should use scope extra")
	assertEqual(t, processedEvent.User, scope.user, "should use scope user")
	assertEqual(t, processedEvent.Fingerprint, scope.fingerprint, "should use scope fingerprint")
	assertEqual(t, processedEvent.Level, scope.level, "should use scope level")
}

func TestEventProcessorsModifiesEvent(t *testing.T) {
	scope := &Scope{}
	event := &Event{}
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
		t.Error("event should not be dropped")
	}
	assertEqual(t, LevelFatal, processedEvent.Level)
	assertEqual(t, []string{"wat"}, processedEvent.Fingerprint)
}

func TestEventProcessorsCanDropEvent(t *testing.T) {
	scope := &Scope{}
	event := &Event{}
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
	scope := &Scope{}
	event := &Event{}
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
