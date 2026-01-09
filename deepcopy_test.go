package sentry

import (
	"reflect"
	"testing"
	"time"
)

func TestDeepCopyStruct(t *testing.T) {
	type payload struct {
		Numbers []int
		Labels  map[string]string
	}

	original := payload{
		Numbers: []int{1, 2},
		Labels:  map[string]string{"a": "b"},
	}

	cloned := deepCopyValue(original).(payload)

	if !reflect.DeepEqual(original, cloned) {
		t.Fatalf("cloned struct differs: got %#v want %#v", cloned, original)
	}

	original.Labels["a"] = "changed"
	original.Numbers[0] = 42

	if cloned.Labels["a"] != "b" {
		t.Fatalf("map value was mutated through original copy")
	}
	if cloned.Numbers[0] != 1 {
		t.Fatalf("slice value was mutated through original copy")
	}
}

func TestDeepCopyPointerCycle(t *testing.T) {
	type node struct {
		Value int
		Next  *node
	}

	original := &node{Value: 1}
	original.Next = original

	cloned := deepCopyValue(original).(*node)

	if cloned == original {
		t.Fatalf("pointer was not copied")
	}
	if cloned.Next != cloned {
		t.Fatalf("cycle was not preserved in clone")
	}
	if cloned.Value != 1 {
		t.Fatalf("unexpected value %d", cloned.Value)
	}
}

func TestDeepCopyMapPointerKey(t *testing.T) {
	type key struct {
		N int
	}

	k := &key{N: 1}
	original := map[*key]string{k: "value"}

	cloned := deepCopyValue(original).(map[*key]string)

	if len(cloned) != 1 {
		t.Fatalf("unexpected map length %d", len(cloned))
	}

	var clonedKey *key
	for ck := range cloned {
		clonedKey = ck
	}

	if clonedKey == k {
		t.Fatalf("map key pointer was not copied")
	}
	if cloned[clonedKey] != "value" {
		t.Fatalf("map value mismatch: %q", cloned[clonedKey])
	}
}

func TestDeepCopyInterfaceValue(t *testing.T) {
	type container struct {
		Value any
	}

	original := container{Value: []int{1, 2, 3}}
	cloned := deepCopyValue(original).(container)

	origSlice := original.Value.([]int)
	clonedSlice := cloned.Value.([]int)

	if &origSlice[0] == &clonedSlice[0] {
		t.Fatalf("interface slice shares backing array")
	}

	origSlice[0] = 99
	if clonedSlice[0] != 1 {
		t.Fatalf("cloned slice mutated through original interface value")
	}
}

func TestDeepCopyArray(t *testing.T) {
	original := [2][]int{{1, 2}, {3}}
	cloned := deepCopyValue(original).([2][]int)

	original[0][0] = 100

	if cloned[0][0] != 1 {
		t.Fatalf("array element was mutated through original copy")
	}
}

func TestDeepCopySlicesSharingBackingArray(t *testing.T) {
	base := []int{1, 2, 3}
	a := base[:2]
	b := base[:3]

	type payload struct {
		A []int
		B []int
	}

	cloned := deepCopyValue(payload{A: a, B: b}).(payload)

	if len(cloned.A) != len(a) || len(cloned.B) != len(b) {
		t.Fatalf("lengths not preserved: A %d B %d", len(cloned.A), len(cloned.B))
	}
	if &cloned.A[0] == &cloned.B[0] {
		t.Fatalf("slices share backing array after copy")
	}

	cloned.A[0] = 99
	if cloned.B[0] == 99 {
		t.Fatalf("mutation leaked between slices after copy")
	}
}

func TestDeepCopyStructWithUnexportedFields(t *testing.T) {
	type container struct {
		t time.Time
	}

	now := time.Now()
	original := container{t: now}

	cloned := deepCopyValue(original).(container)

	if !cloned.t.Equal(now) {
		t.Fatalf("unexported field not copied: got %v want %v", cloned.t, now)
	}
}
