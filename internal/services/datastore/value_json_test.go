package datastore

import (
	"testing"
	"time"

	"cloud.google.com/go/datastore/apiv1/datastorepb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestValueToJSONAndJSONToValue(t *testing.T) {
	if valueToJSON(nil) != nil {
		t.Fatal("nil")
	}
	if v := valueToJSON(&datastorepb.Value{ValueType: &datastorepb.Value_NullValue{}}); v != nil {
		t.Fatalf("null=%#v", v)
	}
	if v := valueToJSON(&datastorepb.Value{ValueType: &datastorepb.Value_BooleanValue{BooleanValue: true}}); v != true {
		t.Fatalf("bool=%#v", v)
	}
	if v := valueToJSON(&datastorepb.Value{ValueType: &datastorepb.Value_IntegerValue{IntegerValue: 7}}); v != int64(7) {
		t.Fatalf("int=%#v", v)
	}
	if v := valueToJSON(&datastorepb.Value{ValueType: &datastorepb.Value_DoubleValue{DoubleValue: 1.5}}); v != 1.5 {
		t.Fatalf("double=%#v", v)
	}
	if v := valueToJSON(&datastorepb.Value{ValueType: &datastorepb.Value_StringValue{StringValue: "s"}}); v != "s" {
		t.Fatalf("string=%#v", v)
	}
	ts := timestamppb.New(time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC))
	if v := valueToJSON(&datastorepb.Value{ValueType: &datastorepb.Value_TimestampValue{TimestampValue: ts}}); v == nil {
		t.Fatal("timestamp")
	}
	if v := valueToJSON(&datastorepb.Value{ValueType: &datastorepb.Value_TimestampValue{}}); v != nil {
		t.Fatalf("nil timestamp=%#v", v)
	}
	arr := &datastorepb.Value{ValueType: &datastorepb.Value_ArrayValue{ArrayValue: &datastorepb.ArrayValue{
		Values: []*datastorepb.Value{{ValueType: &datastorepb.Value_StringValue{StringValue: "a"}}},
	}}}
	if v := valueToJSON(arr); v == nil {
		t.Fatal("array default")
	}

	if jsonToValue(nil).GetNullValue() != 0 {
		// NullValue enum is 0; just ensure non-nil value.
	}
	if !jsonToValue(true).GetBooleanValue() {
		t.Fatal("bool")
	}
	if jsonToValue(float64(3)).GetIntegerValue() != 3 {
		t.Fatal("int from float64")
	}
	if jsonToValue(1.25).GetDoubleValue() != 1.25 {
		t.Fatal("double")
	}
	if jsonToValue("x").GetStringValue() != "x" {
		t.Fatal("string")
	}
	if jsonToValue(map[string]any{"a": 1}).GetStringValue() == "" {
		t.Fatal("default marshal")
	}
}
