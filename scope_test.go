package sentry

import (
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

type ScopeSuite struct {
	suite.Suite
	scope *Scope
	event *Event
}

func TestScopeSuite(t *testing.T) {
	suite.Run(t, new(ScopeSuite))
}

func (suite *ScopeSuite) SetupTest() {
	suite.scope = &Scope{}
	suite.event = &Event{}
}

func (suite *ScopeSuite) FillScopeWithValidData() {
	suite.scope.breadcrumbs = []*Breadcrumb{{Timestamp: 1337, Message: "scopeBreadcrumbMessage"}}
	suite.scope.user = User{ID: "1337"}
	suite.scope.tags = map[string]string{"scopeTagKey": "scopeTagValue"}
	suite.scope.extra = map[string]interface{}{"scopeExtraKey": "scopeExtraValue"}
	suite.scope.fingerprint = []string{"scopeFingerprintOne", "scopeFingerprintTwo"}
	suite.scope.level = LevelDebug
}

func (suite *ScopeSuite) FillEventWithValidData() {
	suite.event.Breadcrumbs = []*Breadcrumb{{Timestamp: 1337, Message: "eventBreadcrumbMessage"}}
	suite.event.User = User{ID: "42"}
	suite.event.Tags = map[string]string{"eventTagKey": "eventTagValue"}
	suite.event.Extra = map[string]interface{}{"eventExtraKey": "eventExtraValue"}
	suite.event.Fingerprint = []string{"eventFingerprintOne", "eventFingerprintTwo"}
	suite.event.Level = LevelInfo
}

func (suite *ScopeSuite) TestSetUser() {
	suite.scope.SetUser(User{ID: "foo"})
	suite.Equal(User{ID: "foo"}, suite.scope.user)
}

func (suite *ScopeSuite) TestSetUserOverrides() {
	suite.scope.SetUser(User{ID: "foo"})
	suite.scope.SetUser(User{ID: "bar"})

	suite.Equal(User{ID: "bar"}, suite.scope.user)
}

func (suite *ScopeSuite) TestSetTag() {
	suite.scope.SetTag("a", "foo")

	suite.Equal(map[string]string{"a": "foo"}, suite.scope.tags)
}

func (suite *ScopeSuite) TestSetTagMerges() {
	suite.scope.SetTag("a", "foo")
	suite.scope.SetTag("b", "bar")

	suite.Equal(map[string]string{"a": "foo", "b": "bar"}, suite.scope.tags)
}

func (suite *ScopeSuite) TestSetTagOverrides() {
	suite.scope.SetTag("a", "foo")
	suite.scope.SetTag("a", "bar")

	suite.Equal(map[string]string{"a": "bar"}, suite.scope.tags)
}

func (suite *ScopeSuite) TestSetTags() {
	suite.scope.SetTags(map[string]string{"a": "foo"})

	suite.Equal(map[string]string{"a": "foo"}, suite.scope.tags)
}
func (suite *ScopeSuite) TestSetTagsMerges() {
	suite.scope.SetTags(map[string]string{"a": "foo"})
	suite.scope.SetTags(map[string]string{"b": "bar", "c": "baz"})

	suite.Equal(map[string]string{"a": "foo", "b": "bar", "c": "baz"}, suite.scope.tags)
}

func (suite *ScopeSuite) TestSetTagsOverrides() {
	suite.scope.SetTags(map[string]string{"a": "foo"})
	suite.scope.SetTags(map[string]string{"a": "bar", "b": "baz"})

	suite.Equal(map[string]string{"a": "bar", "b": "baz"}, suite.scope.tags)
}

func (suite *ScopeSuite) TestSetExtra() {
	suite.scope.SetExtra("a", 1)

	suite.Equal(map[string]interface{}{"a": 1}, suite.scope.extra)
}
func (suite *ScopeSuite) TestSetExtraMerges() {
	suite.scope.SetExtra("a", "foo")
	suite.scope.SetExtra("b", 2)

	suite.Equal(map[string]interface{}{"a": "foo", "b": 2}, suite.scope.extra)
}

func (suite *ScopeSuite) TestSetExtraOverrides() {
	suite.scope.SetExtra("a", "foo")
	suite.scope.SetExtra("a", 2)

	suite.Equal(map[string]interface{}{"a": 2}, suite.scope.extra)
}

func (suite *ScopeSuite) TestSetExtras() {
	suite.scope.SetExtras(map[string]interface{}{"a": 1})

	suite.Equal(map[string]interface{}{"a": 1}, suite.scope.extra)
}
func (suite *ScopeSuite) TestSetExtrasMerges() {
	suite.scope.SetExtras(map[string]interface{}{"a": "foo"})
	suite.scope.SetExtras(map[string]interface{}{"b": 2, "c": 3})

	suite.Equal(map[string]interface{}{"a": "foo", "b": 2, "c": 3}, suite.scope.extra)
}

func (suite *ScopeSuite) TestSetExtrasOverrides() {
	suite.scope.SetExtras(map[string]interface{}{"a": "foo"})
	suite.scope.SetExtras(map[string]interface{}{"a": 2, "b": 3})

	suite.Equal(map[string]interface{}{"a": 2, "b": 3}, suite.scope.extra)
}
func (suite *ScopeSuite) TestSetFingerprint() {
	suite.scope.SetFingerprint([]string{"abcd"})

	suite.Equal([]string{"abcd"}, suite.scope.fingerprint)
}

func (suite *ScopeSuite) TestSetFingerprintOverrides() {
	suite.scope.SetFingerprint([]string{"abc"})
	suite.scope.SetFingerprint([]string{"def"})

	suite.Equal([]string{"def"}, suite.scope.fingerprint)
}

func (suite *ScopeSuite) TestSetLevel() {
	suite.scope.SetLevel(LevelInfo)

	suite.Equal(LevelInfo, suite.scope.level)
}

func (suite *ScopeSuite) TestSetLevelOverrides() {
	suite.scope.SetLevel(LevelInfo)
	suite.scope.SetLevel(LevelFatal)

	suite.Equal(LevelFatal, suite.scope.level)
}

func (suite *ScopeSuite) TestAddBreadcrumb() {
	suite.scope.AddBreadcrumb(&Breadcrumb{Timestamp: 1337, Message: "test"})

	suite.Equal([]*Breadcrumb{{Timestamp: 1337, Message: "test"}}, suite.scope.breadcrumbs)
}

func (suite *ScopeSuite) TestAddBreadcrumbAppends() {
	suite.scope.AddBreadcrumb(&Breadcrumb{Timestamp: 1337, Message: "test1"})
	suite.scope.AddBreadcrumb(&Breadcrumb{Timestamp: 1337, Message: "test2"})
	suite.scope.AddBreadcrumb(&Breadcrumb{Timestamp: 1337, Message: "test3"})

	suite.Equal([]*Breadcrumb{
		{Timestamp: 1337, Message: "test1"},
		{Timestamp: 1337, Message: "test2"},
		{Timestamp: 1337, Message: "test3"},
	}, suite.scope.breadcrumbs)
}

func (suite *ScopeSuite) TestAddBreadcrumbDefaultLimit() {
	for i := 0; i < 101; i++ {
		suite.scope.AddBreadcrumb(&Breadcrumb{Timestamp: 1337, Message: "test"})
	}

	suite.Len(suite.scope.breadcrumbs, 100)
}

func (suite *ScopeSuite) TestAddBreadcrumbAddsTimestamp() {
	suite.scope.AddBreadcrumb(&Breadcrumb{Message: "test"})
	// I know it's not perfect, but mocking time method for one test would be an overkill
	// And adding new breadcrumb will definitely take less than a second — Kamil
	suite.InDelta(time.Now().Unix(), suite.scope.breadcrumbs[0].Timestamp, 1)
}

func (suite *ScopeSuite) TestBasicInheritance() {
	suite.scope.SetExtra("a", 1)

	clone := suite.scope.Clone()

	suite.Equal(suite.scope.extra, clone.extra)
}

func (suite *ScopeSuite) TestParentChangedInheritance() {
	clone := suite.scope.Clone()

	clone.SetTag("foo", "bar")
	clone.SetExtra("foo", "bar")
	clone.SetLevel(LevelDebug)
	clone.SetFingerprint([]string{"foo"})
	clone.AddBreadcrumb(&Breadcrumb{Timestamp: 1337, Message: "foo"})
	clone.SetUser(User{ID: "foo"})

	suite.scope.SetTag("foo", "baz")
	suite.scope.SetExtra("foo", "baz")
	suite.scope.SetLevel(LevelFatal)
	suite.scope.SetFingerprint([]string{"bar"})
	suite.scope.AddBreadcrumb(&Breadcrumb{Timestamp: 1337, Message: "bar"})
	suite.scope.SetUser(User{ID: "bar"})

	suite.Equal(map[string]string{"foo": "bar"}, clone.tags)
	suite.Equal(map[string]interface{}{"foo": "bar"}, clone.extra)
	suite.Equal(LevelDebug, clone.level)
	suite.Equal([]string{"foo"}, clone.fingerprint)
	suite.Equal([]*Breadcrumb{{Timestamp: 1337, Message: "foo"}}, clone.breadcrumbs)
	suite.Equal(User{ID: "foo"}, clone.user)

	suite.Equal(map[string]string{"foo": "baz"}, suite.scope.tags)
	suite.Equal(map[string]interface{}{"foo": "baz"}, suite.scope.extra)
	suite.Equal(LevelFatal, suite.scope.level)
	suite.Equal([]string{"bar"}, suite.scope.fingerprint)
	suite.Equal([]*Breadcrumb{{Timestamp: 1337, Message: "bar"}}, suite.scope.breadcrumbs)
	suite.Equal(User{ID: "bar"}, suite.scope.user)
}

func (suite *ScopeSuite) TestChildOverrideInheritance() {
	suite.scope.SetTag("foo", "baz")
	suite.scope.SetExtra("foo", "baz")
	suite.scope.SetLevel(LevelFatal)
	suite.scope.SetFingerprint([]string{"bar"})
	suite.scope.AddBreadcrumb(&Breadcrumb{Timestamp: 1337, Message: "bar"})
	suite.scope.SetUser(User{ID: "bar"})

	clone := suite.scope.Clone()
	clone.SetTag("foo", "bar")
	clone.SetExtra("foo", "bar")
	clone.SetLevel(LevelDebug)
	clone.SetFingerprint([]string{"foo"})
	clone.AddBreadcrumb(&Breadcrumb{Timestamp: 1337, Message: "foo"})
	clone.SetUser(User{ID: "foo"})

	suite.Equal(map[string]string{"foo": "bar"}, clone.tags)
	suite.Equal(map[string]interface{}{"foo": "bar"}, clone.extra)
	suite.Equal(LevelDebug, clone.level)
	suite.Equal([]string{"foo"}, clone.fingerprint)
	suite.Equal([]*Breadcrumb{{Timestamp: 1337, Message: "bar"}, {Timestamp: 1337, Message: "foo"}}, clone.breadcrumbs)
	suite.Equal(User{ID: "foo"}, clone.user)

	suite.Equal(map[string]string{"foo": "baz"}, suite.scope.tags)
	suite.Equal(map[string]interface{}{"foo": "baz"}, suite.scope.extra)
	suite.Equal(LevelFatal, suite.scope.level)
	suite.Equal([]string{"bar"}, suite.scope.fingerprint)
	suite.Equal([]*Breadcrumb{{Timestamp: 1337, Message: "bar"}}, suite.scope.breadcrumbs)
	suite.Equal(User{ID: "bar"}, suite.scope.user)
}

func (suite *ScopeSuite) TestClear() {
	suite.FillScopeWithValidData()

	suite.scope.Clear()

	suite.Equal([]*Breadcrumb(nil), suite.scope.breadcrumbs)
	suite.Equal(User{}, suite.scope.user)
	suite.Equal(map[string]string(nil), suite.scope.tags)
	suite.Equal(map[string]interface{}(nil), suite.scope.extra)
	suite.Equal([]string(nil), suite.scope.fingerprint)
	suite.Equal(Level(""), suite.scope.level)
}

func (suite *ScopeSuite) TestClearBreadcrumbs() {
	suite.FillScopeWithValidData()

	suite.scope.ClearBreadcrumbs()

	suite.Equal([]*Breadcrumb{}, suite.scope.breadcrumbs)
}

func (suite *ScopeSuite) TestApplyToEvent() {
	suite.FillScopeWithValidData()
	suite.FillEventWithValidData()

	processedEvent := suite.scope.ApplyToEvent(suite.event)

	suite.Len(processedEvent.Breadcrumbs, 2, "should merge breadcrumbs")
	suite.Len(processedEvent.Tags, 2, "should merge tags")
	suite.Len(processedEvent.Extra, 2, "should merge extra")
	suite.Equal(processedEvent.Level, suite.scope.level, "should use scope level if its set")
	suite.NotEqual(processedEvent.User, suite.scope.user, "should use event user if one exist")
	suite.NotEqual(processedEvent.Fingerprint, suite.scope.fingerprint, "should use event fingerprints if they exist")
}

func (suite *ScopeSuite) TestApplyToEventEmptyScope() {
	suite.FillEventWithValidData()

	processedEvent := suite.scope.ApplyToEvent(suite.event)

	suite.True(true, "Shoudn't blow up")
	suite.Len(processedEvent.Breadcrumbs, 1, "should use event breadcrumbs")
	suite.Len(processedEvent.Tags, 1, "should use event tags")
	suite.Len(processedEvent.Extra, 1, "should use event extra")
	suite.NotEqual(processedEvent.User, suite.scope.user, "should use event user")
	suite.NotEqual(processedEvent.Fingerprint, suite.scope.fingerprint, "should use event fingerprint")
	suite.NotEqual(processedEvent.Level, suite.scope.level, "should use event level")
}

func (suite *ScopeSuite) TestApplyToEventEmptyEvent() {
	suite.FillScopeWithValidData()

	processedEvent := suite.scope.ApplyToEvent(suite.event)

	suite.True(true, "Shoudn't blow up")
	suite.Len(processedEvent.Breadcrumbs, 1, "should use scope breadcrumbs")
	suite.Len(processedEvent.Tags, 1, "should use scope tags")
	suite.Len(processedEvent.Extra, 1, "should use scope extra")
	suite.Equal(processedEvent.User, suite.scope.user, "should use scope user")
	suite.Equal(processedEvent.Fingerprint, suite.scope.fingerprint, "should use scope fingerprint")
	suite.Equal(processedEvent.Level, suite.scope.level, "should use scope level")
}

func (suite *ScopeSuite) TestApplyToEventLimitBreadcrumbs() {
	for i := 0; i < 101; i++ {
		suite.scope.AddBreadcrumb(&Breadcrumb{Timestamp: 1337, Message: "test"})
	}
	suite.event.Breadcrumbs = []*Breadcrumb{{Timestamp: 1337, Message: "foo"}, {Timestamp: 1337, Message: "bar"}}

	processedEvent := suite.scope.ApplyToEvent(suite.event)

	suite.Len(processedEvent.Breadcrumbs, 100)
	suite.Equal(&Breadcrumb{Timestamp: 1337, Message: "foo"}, processedEvent.Breadcrumbs[0])
	suite.Equal(&Breadcrumb{Timestamp: 1337, Message: "bar"}, processedEvent.Breadcrumbs[1])
}

func (suite *ScopeSuite) TestEventProcessors() {
	suite.scope.eventProcessors = []EventProcessor{
		func(event *Event) *Event {
			event.Level = LevelFatal
			return event
		},
		func(event *Event) *Event {
			event.Fingerprint = []string{"wat"}
			return event
		},
	}

	processedEvent := suite.scope.ApplyToEvent(suite.event)

	suite.NotNil(processedEvent)
	suite.Equal(LevelFatal, processedEvent.Level)
	suite.Equal([]string{"wat"}, processedEvent.Fingerprint)
}

func (suite *ScopeSuite) TestEventProcessorsCanDropEvent() {
	suite.scope.eventProcessors = []EventProcessor{
		func(event *Event) *Event {
			return nil
		},
	}

	processedEvent := suite.scope.ApplyToEvent(suite.event)

	suite.Nil(processedEvent)
}

func (suite *ScopeSuite) TestAddEventProcessor() {
	processedEvent := suite.scope.ApplyToEvent(suite.event)
	suite.NotNil(processedEvent)

	suite.scope.AddEventProcessor(func(event *Event) *Event {
		return nil
	})

	processedEvent = suite.scope.ApplyToEvent(suite.event)
	suite.Nil(processedEvent)
}
