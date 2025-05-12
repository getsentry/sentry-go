package attribute

import (
	"testing"
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
	} {
		if testcase.value.Value.Type() != testcase.wantType {
			t.Errorf("wrong value type, got %#v, expected %#v", testcase.value.Value.Type(), testcase.wantType)
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
	} {
		valid := tt.builder.Valid()
		if tt.wantValid != valid {
			t.Errorf("expected builder valid to be %v, got %v", tt.wantValid, valid)
		}
	}
}
