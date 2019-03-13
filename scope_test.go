package sentry

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetUser(t *testing.T) {
	scope := NewScope()
	scope.SetUser(User{id: "foo"})
	assert.Equal(t, User{id: "foo"}, scope.user)
}

func TestSetUserOverrides(t *testing.T) {
	scope := NewScope()
	scope.SetUser(User{id: "foo"})
	scope.SetUser(User{id: "bar"})
	assert.Equal(t, User{id: "bar"}, scope.user)
}

func TestSetTag(t *testing.T) {
	scope := NewScope()
	scope.SetTag("a", "foo")
	assert.Equal(t, map[string]string{"a": "foo"}, scope.tags)
}

func TestSetTagMerges(t *testing.T) {
	scope := NewScope()
	scope.SetTag("a", "foo")
	scope.SetTag("b", "bar")
	assert.Equal(t, map[string]string{"a": "foo", "b": "bar"}, scope.tags)
}

func TestSetTagOverrides(t *testing.T) {
	scope := NewScope()
	scope.SetTag("a", "foo")
	scope.SetTag("a", "bar")
	assert.Equal(t, map[string]string{"a": "bar"}, scope.tags)
}

func TestSetTags(t *testing.T) {
	scope := NewScope()
	scope.SetTags(map[string]string{"a": "foo"})
	assert.Equal(t, map[string]string{"a": "foo"}, scope.tags)
}
func TestSetTagsMerges(t *testing.T) {
	scope := NewScope()
	scope.SetTags(map[string]string{"a": "foo"})
	scope.SetTags(map[string]string{"b": "bar", "c": "baz"})
	assert.Equal(t, map[string]string{"a": "foo", "b": "bar", "c": "baz"}, scope.tags)
}

func TestSetTagsOverrides(t *testing.T) {
	scope := NewScope()
	scope.SetTags(map[string]string{"a": "foo"})
	scope.SetTags(map[string]string{"a": "bar", "b": "baz"})
	assert.Equal(t, map[string]string{"a": "bar", "b": "baz"}, scope.tags)
}

func TestSetExtra(t *testing.T) {
	scope := NewScope()
	scope.SetExtra("a", 1)
	assert.Equal(t, map[string]interface{}{"a": 1}, scope.extra)
}
func TestSetExtraMerges(t *testing.T) {
	scope := NewScope()
	scope.SetExtra("a", "foo")
	scope.SetExtra("b", 2)
	assert.Equal(t, map[string]interface{}{"a": "foo", "b": 2}, scope.extra)
}

func TestSetExtraOverrides(t *testing.T) {
	scope := NewScope()
	scope.SetExtra("a", "foo")
	scope.SetExtra("a", 2)
	assert.Equal(t, map[string]interface{}{"a": 2}, scope.extra)
}

func TestSetExtras(t *testing.T) {
	scope := NewScope()
	scope.SetExtras(map[string]interface{}{"a": 1})
	assert.Equal(t, map[string]interface{}{"a": 1}, scope.extra)
}
func TestSetExtrasMerges(t *testing.T) {
	scope := NewScope()
	scope.SetExtras(map[string]interface{}{"a": "foo"})
	scope.SetExtras(map[string]interface{}{"b": 2, "c": 3})
	assert.Equal(t, map[string]interface{}{"a": "foo", "b": 2, "c": 3}, scope.extra)
}

func TestSetExtrasOverrides(t *testing.T) {
	scope := NewScope()
	scope.SetExtras(map[string]interface{}{"a": "foo"})
	scope.SetExtras(map[string]interface{}{"a": 2, "b": 3})
	assert.Equal(t, map[string]interface{}{"a": 2, "b": 3}, scope.extra)
}
func TestSetFingerprint(t *testing.T) {
	scope := NewScope()
	scope.SetFingerprint([]string{"abcd"})
	assert.Equal(t, []string{"abcd"}, scope.fingerprint)
}

func TestSetFingerprintOverrides(t *testing.T) {
	scope := NewScope()
	scope.SetFingerprint([]string{"abc"})
	scope.SetFingerprint([]string{"def"})
	assert.Equal(t, []string{"def"}, scope.fingerprint)
}

func TestSetLevel(t *testing.T) {
	scope := NewScope()
	scope.SetLevel(LevelInfo)
	assert.Equal(t, LevelInfo, scope.level)
}

func TestSetLevelOverrides(t *testing.T) {
	scope := NewScope()
	scope.SetLevel(LevelInfo)
	scope.SetLevel(LevelFatal)
	assert.Equal(t, LevelFatal, scope.level)
}

func TestAddBreadcrumb(t *testing.T) {
	scope := NewScope()
	scope.AddBreadcrumb(Breadcrumb{message: "test"})
	assert.Equal(t, []Breadcrumb{{message: "test"}}, scope.breadcrumbs)
}

func TestAddBreadcrumbAppends(t *testing.T) {
	scope := NewScope()
	scope.AddBreadcrumb(Breadcrumb{message: "test1"})
	scope.AddBreadcrumb(Breadcrumb{message: "test2"})
	scope.AddBreadcrumb(Breadcrumb{message: "test3"})
	assert.Equal(t, []Breadcrumb{{message: "test1"}, {message: "test2"}, {message: "test3"}}, scope.breadcrumbs)
}

func TestAddBreadcrumbDefaultLimit(t *testing.T) {
	scope := NewScope()
	for i := 0; i < 101; i++ {
		scope.AddBreadcrumb(Breadcrumb{message: "test"})
	}
	assert.Equal(t, 100, len(scope.breadcrumbs))
}

func TestBasicInheritance(t *testing.T) {
	parentScope := NewScope()
	parentScope.SetExtra("a", 1)
	scope := parentScope.Clone()
	assert.Equal(t, parentScope.extra, scope.extra)
}

// TODO: TEST OTHER
func TestParentChangedInheritance(t *testing.T) {
	parentScope := NewScope()
	scope := parentScope.Clone()

	scope.SetTag("foo", "bar")
	scope.SetExtra("foo", "bar")
	scope.SetLevel(LevelDebug)
	scope.SetFingerprint([]string{"foo"})
	scope.AddBreadcrumb(Breadcrumb{message: "foo"})
	scope.SetUser(User{id: "foo"})

	parentScope.SetTag("foo", "baz")
	parentScope.SetExtra("foo", "baz")
	parentScope.SetLevel(LevelFatal)
	parentScope.SetFingerprint([]string{"bar"})
	parentScope.AddBreadcrumb(Breadcrumb{message: "bar"})
	parentScope.SetUser(User{id: "bar"})

	assert.Equal(t, map[string]string{"foo": "bar"}, scope.tags)
	assert.Equal(t, map[string]interface{}{"foo": "bar"}, scope.extra)
	assert.Equal(t, LevelDebug, scope.level)
	assert.Equal(t, []string{"foo"}, scope.fingerprint)
	assert.Equal(t, []Breadcrumb{{message: "foo"}}, scope.breadcrumbs)
	assert.Equal(t, User{id: "foo"}, scope.user)

	assert.Equal(t, map[string]string{"foo": "baz"}, parentScope.tags)
	assert.Equal(t, map[string]interface{}{"foo": "baz"}, parentScope.extra)
	assert.Equal(t, LevelFatal, parentScope.level)
	assert.Equal(t, []string{"bar"}, parentScope.fingerprint)
	assert.Equal(t, []Breadcrumb{{message: "bar"}}, parentScope.breadcrumbs)
	assert.Equal(t, User{id: "bar"}, parentScope.user)
}

func TestChildOverrideInheritance(t *testing.T) {
	parentScope := NewScope()
	parentScope.SetTag("foo", "baz")
	parentScope.SetExtra("foo", "baz")
	parentScope.SetLevel(LevelFatal)
	parentScope.SetFingerprint([]string{"bar"})
	parentScope.AddBreadcrumb(Breadcrumb{message: "bar"})
	parentScope.SetUser(User{id: "bar"})

	scope := parentScope.Clone()
	scope.SetTag("foo", "bar")
	scope.SetExtra("foo", "bar")
	scope.SetLevel(LevelDebug)
	scope.SetFingerprint([]string{"foo"})
	scope.AddBreadcrumb(Breadcrumb{message: "foo"})
	scope.SetUser(User{id: "foo"})

	assert.Equal(t, map[string]string{"foo": "bar"}, scope.tags)
	assert.Equal(t, map[string]interface{}{"foo": "bar"}, scope.extra)
	assert.Equal(t, LevelDebug, scope.level)
	assert.Equal(t, []string{"foo"}, scope.fingerprint)
	assert.Equal(t, []Breadcrumb{{message: "foo"}}, scope.breadcrumbs)
	assert.Equal(t, User{id: "foo"}, scope.user)

	assert.Equal(t, map[string]string{"foo": "baz"}, parentScope.tags)
	assert.Equal(t, map[string]interface{}{"foo": "baz"}, parentScope.extra)
	assert.Equal(t, LevelFatal, parentScope.level)
	assert.Equal(t, []string{"bar"}, parentScope.fingerprint)
	assert.Equal(t, []Breadcrumb{{message: "bar"}}, parentScope.breadcrumbs)
	assert.Equal(t, User{id: "bar"}, parentScope.user)
}

func TestClear(t *testing.T) {
	scope := NewScope()
	scope.AddBreadcrumb(Breadcrumb{message: "test"})
	scope.SetUser(User{id: "1"})
	scope.SetTag("a", "b")
	scope.SetExtra("a", 2)
	scope.SetFingerprint([]string{"abcd"})
	scope.SetLevel(LevelFatal)
	scope.Clear()
	assert.Equal(t, []Breadcrumb{}, scope.breadcrumbs)
	assert.Equal(t, User{}, scope.user)
	assert.Equal(t, map[string]string{}, scope.tags)
	assert.Equal(t, map[string]interface{}{}, scope.extra)
	assert.Equal(t, []string{}, scope.fingerprint)
	assert.Equal(t, LevelInfo, scope.level)
}
