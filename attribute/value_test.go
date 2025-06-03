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
			name:  "Float64Value() correctly returns a float64 value",
			value: Float64Value(42.1),
			want:  "float64",
		},
		{
			name:  "StringValue() correctly returns a string value",
			value: StringValue("foo"),
			want:  "string",
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
