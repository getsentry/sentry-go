package attribute

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestBuilder(t *testing.T) {
	k := "key"
	for _, testcase := range []struct {
		name      string
		value     Builder
		wantType  Type
		wantValue interface{}
	}{
		{
			name:      "Bool correctly returns a boolean builder",
			value:     Bool(k, true),
			wantType:  BOOL,
			wantValue: true,
		},
		{
			name:      "Int64() correctly returns an builder",
			value:     Int64(k, 42),
			wantType:  INT64,
			wantValue: int64(42),
		},
		{
			name:      "Int() correctly returns an builder",
			value:     Int(k, 42),
			wantType:  INT64,
			wantValue: int64(42),
		},
		{
			name:      "Float64() correctly returns a float64 builder",
			value:     Float64(k, 42.1),
			wantType:  FLOAT64,
			wantValue: 42.1,
		},
		{
			name:      "String() correctly returns a string builder",
			value:     String(k, "foo"),
			wantType:  STRING,
			wantValue: "foo",
		},
		{
			name:      "BoolSlice() correctly returns a bool slice builder",
			value:     BoolSlice(k, []bool{true, false, true}),
			wantType:  BOOLSLICE,
			wantValue: []bool{true, false, true},
		},
		{
			name:      "IntSlice() correctly returns an int slice builder",
			value:     IntSlice(k, []int{1, 2, 3}),
			wantType:  INT64SLICE,
			wantValue: []int64{1, 2, 3},
		},
		{
			name:      "Int64Slice() correctly returns an int64 slice builder",
			value:     Int64Slice(k, []int64{42, 43, 44}),
			wantType:  INT64SLICE,
			wantValue: []int64{42, 43, 44},
		},
		{
			name:      "Float64Slice() correctly returns a float64 slice builder",
			value:     Float64Slice(k, []float64{1.5, 2.5, 3.5}),
			wantType:  FLOAT64SLICE,
			wantValue: []float64{1.5, 2.5, 3.5},
		},
		{
			name:      "StringSlice() correctly returns a string slice builder",
			value:     StringSlice(k, []string{"foo", "bar", "baz"}),
			wantType:  STRINGSLICE,
			wantValue: []string{"foo", "bar", "baz"},
		},
	} {
		if testcase.value.Value.Type() != testcase.wantType {
			t.Errorf("wrong value type, got %#v, expected %#v", testcase.value.Value.Type(), testcase.wantType)
		}
		if testcase.value.Key != k {
			t.Errorf("wrong key, got %#v, expected %#v", testcase.value.Key, k)
		}
		got := testcase.value.Value.AsInterface()
		if diff := cmp.Diff(testcase.wantValue, got); diff != "" {
			t.Errorf("+got, -want: %s", diff)
		}
	}
}

func TestBuilder_Valid(t *testing.T) {
	for _, tt := range []struct {
		name      string
		builder   Builder
		wantValid bool
	}{
		{
			name: "valid key and value are valid",
			builder: Builder{
				Key:   "key",
				Value: BoolValue(true),
			},
			wantValid: true,
		},
		{
			name: "empty key should be invalid",
			builder: Builder{
				Key:   "",
				Value: IntValue(42),
			},
			wantValid: false,
		},
		{
			name: "invalid value should be invalid",
			builder: Builder{
				Key:   "key",
				Value: Value{},
			},
			wantValid: false,
		},
		{
			name:      "BoolSlice builder is valid",
			builder:   BoolSlice("key", []bool{true}),
			wantValid: true,
		},
		{
			name:      "Int64Slice builder is valid",
			builder:   Int64Slice("key", []int64{42}),
			wantValid: true,
		},
		{
			name:      "Float64Slice builder is valid",
			builder:   Float64Slice("key", []float64{1.5}),
			wantValid: true,
		},
		{
			name:      "StringSlice builder is valid",
			builder:   StringSlice("key", []string{"foo"}),
			wantValid: true,
		},
	} {
		valid := tt.builder.Valid()
		if tt.wantValid != valid {
			t.Errorf("expected builder valid to be %v, got %v", tt.wantValid, valid)
		}
	}
}
