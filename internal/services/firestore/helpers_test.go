package firestore

import (
	"testing"
	"time"

	"cloud.google.com/go/firestore/apiv1/firestorepb"
)

func TestCompareValuesParseTimeHelpers(t *testing.T) {
	_ = parseTime(time.Now().UTC().Format(time.RFC3339Nano))
	_ = parseTime(time.Now().UTC().Format(time.RFC3339))
	_ = parseTime("not-a-time")
	_ = newDocID()

	a := &firestorepb.Value{ValueType: &firestorepb.Value_IntegerValue{IntegerValue: 1}}
	b := &firestorepb.Value{ValueType: &firestorepb.Value_IntegerValue{IntegerValue: 2}}
	c := &firestorepb.Value{ValueType: &firestorepb.Value_DoubleValue{DoubleValue: 1.5}}
	s1 := &firestorepb.Value{ValueType: &firestorepb.Value_StringValue{StringValue: "a"}}
	s2 := &firestorepb.Value{ValueType: &firestorepb.Value_StringValue{StringValue: "b"}}

	if n, err := compareValues(a, b); err != nil || n >= 0 {
		t.Fatalf("int cmp: %d %v", n, err)
	}
	if n, err := compareValues(b, a); err != nil || n <= 0 {
		t.Fatalf("int cmp2: %d %v", n, err)
	}
	if n, err := compareValues(a, a); err != nil || n != 0 {
		t.Fatalf("eq: %d %v", n, err)
	}
	if _, err := compareValues(a, c); err != nil {
		t.Fatalf("int/double: %v", err)
	}
	if n, err := compareValues(s1, s2); err != nil || n >= 0 {
		t.Fatalf("string: %d %v", n, err)
	}
	if _, err := compareValues(nil, a); err == nil {
		t.Fatal("nil")
	}
	if _, err := compareValues(a, s1); err == nil {
		t.Fatal("mixed types")
	}
	eq, err := valuesEqual(a, a)
	if err != nil || !eq {
		t.Fatal("valuesEqual")
	}
	eq, err = valuesEqual(a, b)
	if err != nil || eq {
		t.Fatal("valuesEqual ne")
	}
	eq, err = valuesEqual(nil, nil)
	if err != nil || !eq {
		t.Fatal("nil equal")
	}
	if _, ok := asNumber(a); !ok {
		t.Fatal("asNumber int")
	}
	if _, ok := asNumber(c); !ok {
		t.Fatal("asNumber double")
	}
	if _, ok := asNumber(s1); ok {
		t.Fatal("asNumber string")
	}
}
