package util

import (
	"reflect"
	"testing"
	"time"
)

func TestDeepCopyStruct(t *testing.T) {
	type payload struct {
		Numbers    []int
		Labels     map[string]string
		unexported map[string]string
	}

	original := payload{
		Numbers:    []int{1, 2},
		Labels:     map[string]string{"a": "b"},
		unexported: map[string]string{"1": "2"},
	}

	cloned := Deepcopy(original).(payload)

	if !reflect.DeepEqual(original, cloned) {
		t.Fatalf("cloned struct differs: got %#v want %#v", cloned, original)
	}

	original.Labels["a"] = "changed"
	original.Numbers[0] = 42
	original.unexported["1"] = "changed"

	if cloned.Labels["a"] != "b" {
		t.Fatalf("exported map was mutated through original")
	}
	if cloned.Numbers[0] != 1 {
		t.Fatalf("exported slice was mutated through original")
	}
	if cloned.unexported["1"] != "2" {
		t.Fatalf("unexported map was mutated through original")
	}
}

func TestDeepCopyPointerCycle(t *testing.T) {
	type node struct {
		Value int
		Next  *node
	}

	original := &node{Value: 1}
	original.Next = original

	cloned := Deepcopy(original).(*node)

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

	cloned := Deepcopy(original).(map[*key]string)

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
	cloned := Deepcopy(original).(container)

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
	cloned := Deepcopy(original).([2][]int)

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

	cloned := Deepcopy(payload{A: a, B: b}).(payload)

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
		m map[string]string
	}

	now := time.Now()
	original := container{t: now, m: map[string]string{"1": "1"}}

	cloned := Deepcopy(original).(container)

	if !cloned.t.Equal(now) {
		t.Fatalf("unexported field not copied: got %v want %v", cloned.t, now)
	}

	original.m["1"] = "2"
	original.m["2"] = "2"

	if cloned.m["1"] != "1" {
		t.Fatalf("unexported map field mutated through original copy")
	}
	if _, ok := cloned.m["2"]; ok {
		t.Fatalf("unexported map field shares backing map with original")
	}
}

func TestDeepCopyStructWithUnexportedSlice(t *testing.T) {
	type container struct {
		data []int
	}

	original := container{data: []int{1, 2, 3}}
	cloned := Deepcopy(original).(container)

	if len(cloned.data) != len(original.data) {
		t.Fatalf("unexpected cloned length %d", len(cloned.data))
	}
	if &cloned.data[0] == &original.data[0] {
		t.Fatalf("unexported slice shares backing array")
	}

	original.data[0] = 99
	if cloned.data[0] != 1 {
		t.Fatalf("unexported slice mutated through original copy")
	}
}

func TestDeepCopyInterfaceContainingUnexportedStruct(t *testing.T) {
	type secret struct {
		t time.Time
		m map[string]string
	}
	type container struct {
		V any
	}

	now := time.Now()
	original := container{
		V: secret{
			t: now,
			m: map[string]string{"a": "1"},
		},
	}

	cloned := Deepcopy(original).(container)
	clonedSecret, ok := cloned.V.(secret)
	if !ok {
		t.Fatalf("unexpected cloned type %T", cloned.V)
	}
	if !clonedSecret.t.Equal(now) {
		t.Fatalf("unexported time not preserved: got %v want %v", clonedSecret.t, now)
	}

	origSecret := original.V.(secret)
	origSecret.m["a"] = "changed"
	origSecret.m["b"] = "new"

	if clonedSecret.m["a"] != "1" {
		t.Fatalf("unexported map mutated through interface copy")
	}
	if _, exists := clonedSecret.m["b"]; exists {
		t.Fatalf("unexported map shares backing map through interface copy")
	}
}
