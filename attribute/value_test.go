// Adapted from https://github.com/open-telemetry/opentelemetry-go/blob/cc43e01c27892252aac9a8f20da28cdde957a289/attribute/value.go
//
// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package attribute

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestValue(t *testing.T) {
	for _, testcase := range []struct {
		name      string
		value     Value
		wantType  Type
		wantValue interface{}
	}{
		{
			name:      "BoolValue correctly returns a boolean",
			value:     BoolValue(true),
			wantType:  BOOL,
			wantValue: true,
		},
		{
			name:      "Int64Value() correctly returns an int64",
			value:     Int64Value(42),
			wantType:  INT64,
			wantValue: int64(42),
		},
		{
			name:      "IntValue() correctly returns an int64",
			value:     IntValue(42),
			wantType:  INT64,
			wantValue: int64(42),
		},
		{
			name:      "Float64Value() correctly returns a float64 value",
			value:     Float64Value(42.1),
			wantType:  FLOAT64,
			wantValue: 42.1,
		},
		{
			name:      "StringValue() correctly returns a string value",
			value:     StringValue("foo"),
			wantType:  STRING,
			wantValue: "foo",
		},
		{
			name:      "BoolSliceValue() correctly returns a bool slice value",
			value:     BoolSliceValue([]bool{true, false, true}),
			wantType:  BOOLSLICE,
			wantValue: []bool{true, false, true},
		},
		{
			name:      "IntSliceValue() correctly returns an int slice value as int64",
			value:     IntSliceValue([]int{1, 2, 3}),
			wantType:  INT64SLICE,
			wantValue: []int64{1, 2, 3},
		},
		{
			name:      "Int64SliceValue() correctly returns an int64 slice value",
			value:     Int64SliceValue([]int64{42, 43, 44}),
			wantType:  INT64SLICE,
			wantValue: []int64{42, 43, 44},
		},
		{
			name:      "Float64SliceValue() correctly returns a float64 slice value",
			value:     Float64SliceValue([]float64{1.5, 2.5, 3.5}),
			wantType:  FLOAT64SLICE,
			wantValue: []float64{1.5, 2.5, 3.5},
		},
		{
			name:      "StringSliceValue() correctly returns a string slice value",
			value:     StringSliceValue([]string{"foo", "bar"}),
			wantType:  STRINGSLICE,
			wantValue: []string{"foo", "bar"},
		},
		{
			name:      "Invalid value type",
			value:     Value{},
			wantType:  INVALID,
			wantValue: unknownValueType{},
		},
	} {
		if testcase.value.Type() != testcase.wantType {
			t.Errorf("wrong value type, got %#v, expected %#v", testcase.value.Type(), testcase.wantType)
		}
		got := testcase.value.AsInterface()
		if diff := cmp.Diff(testcase.wantValue, got); diff != "" {
			t.Errorf("+got, -want: %s", diff)
		}
	}
}

func TestValue_toString(t *testing.T) {
	for _, tt := range []struct {
		name  string
		value Value
		want  string
	}{
		{
			name:  "StringValue",
			value: StringValue("foo"),
			want:  "foo",
		},
		{
			name:  "Int64Value",
			value: Int64Value(42),
			want:  "42",
		},
		{
			name:  "IntValue",
			value: IntValue(42),
			want:  "42",
		},
		{
			name:  "Float64Value",
			value: Float64Value(42.1),
			want:  "42.1",
		},
		{
			name:  "BoolValue",
			value: BoolValue(true),
			want:  "true",
		},
		{
			name:  "BoolSliceValue",
			value: BoolSliceValue([]bool{true, false}),
			want:  "[true false]",
		},
		{
			name:  "IntSliceValue",
			value: IntSliceValue([]int{1, 2, 3}),
			want:  "[1 2 3]",
		},
		{
			name:  "Int64SliceValue",
			value: Int64SliceValue([]int64{42, 43}),
			want:  "[42 43]",
		},
		{
			name:  "Float64SliceValue",
			value: Float64SliceValue([]float64{1.5, 2.5}),
			want:  "[1.5 2.5]",
		},
		{
			name:  "StringSliceValue",
			value: StringSliceValue([]string{"foo", "bar"}),
			want:  "[foo bar]",
		},
		{
			name:  "Invalid Value",
			value: Value{},
			want:  "unknown",
		},
	} {
		str := tt.value.String()
		if str != tt.want {
			t.Errorf("expected %v, got: %v", tt.want, str)
		}
	}
}

func TestValue_ImmutableSlices(t *testing.T) {
	// Test that modifying the original slice doesn't affect the stored value
	t.Run("BoolSlice immutability", func(t *testing.T) {
		original := []bool{true, false, true}
		value := BoolSliceValue(original)
		original[0] = false // Modify original

		got := value.AsBoolSlice()
		want := []bool{true, false, true}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("original slice mutation affected stored value: %s", diff)
		}
	})

	t.Run("Int64Slice immutability", func(t *testing.T) {
		original := []int64{1, 2, 3}
		value := Int64SliceValue(original)
		original[0] = 999 // Modify original

		got := value.AsInt64Slice()
		want := []int64{1, 2, 3}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("original slice mutation affected stored value: %s", diff)
		}
	})

	t.Run("Float64Slice immutability", func(t *testing.T) {
		original := []float64{1.5, 2.5, 3.5}
		value := Float64SliceValue(original)
		original[0] = 999.9 // Modify original

		got := value.AsFloat64Slice()
		want := []float64{1.5, 2.5, 3.5}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("original slice mutation affected stored value: %s", diff)
		}
	})

	t.Run("StringSlice immutability", func(t *testing.T) {
		original := []string{"foo", "bar", "baz"}
		value := StringSliceValue(original)
		original[0] = "modified" // Modify original

		got := value.AsStringSlice()
		want := []string{"foo", "bar", "baz"}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("original slice mutation affected stored value: %s", diff)
		}
	})
}

func TestType_String(t *testing.T) {
	for _, testcase := range []struct {
		name  string
		value Value
		want  string
	}{
		{
			name:  "BoolValue correctly returns a boolean",
			value: BoolValue(true),
			want:  "bool",
		},
		{
			name:  "BoolSliceValue correctly returns a bool slice",
			value: BoolSliceValue([]bool{true}),
			want:  "boolslice",
		},
		{
			name:  "Int64Value() correctly returns an int64",
			value: Int64Value(42),
			want:  "int64",
		},
		{
			name:  "IntValue() correctly returns an int64",
			value: IntValue(42),
			want:  "int64",
		},
		{
			name:  "Int64SliceValue() correctly returns an int64 slice",
			value: Int64SliceValue([]int64{42}),
			want:  "int64slice",
		},
		{
			name:  "Float64Value() correctly returns a float64 value",
			value: Float64Value(42.1),
			want:  "float64",
		},
		{
			name:  "Float64SliceValue() correctly returns a float64 slice",
			value: Float64SliceValue([]float64{42.1}),
			want:  "float64slice",
		},
		{
			name:  "StringValue() correctly returns a string value",
			value: StringValue("foo"),
			want:  "string",
		},
		{
			name:  "StringSliceValue() correctly returns a string slice",
			value: StringSliceValue([]string{"foo"}),
			want:  "stringslice",
		},
		{
			name:  "Invalid value returns INVALID",
			value: Value{},
			want:  "invalid",
		},
	} {
		if testcase.value.Type().String() != testcase.want {
			t.Errorf("wrong value type, got %#v, expected %#v", testcase.value.Type().String(), testcase.want)
		}
	}
}
