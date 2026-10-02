package managedkafka

import (
	"encoding/json"
	"testing"
)

func TestAsIntBranches(t *testing.T) {
	if v, ok := asInt(float64(7)); !ok || v != 7 {
		t.Fatalf("float64 %v %v", v, ok)
	}
	if v, ok := asInt(int(4)); !ok || v != 4 {
		t.Fatalf("int %v %v", v, ok)
	}
	if v, ok := asInt(int64(9)); !ok || v != 9 {
		t.Fatalf("int64 %v %v", v, ok)
	}
	if v, ok := asInt(json.Number("11")); !ok || v != 11 {
		t.Fatalf("json.Number %v %v", v, ok)
	}
	if _, ok := asInt(json.Number("nope")); ok {
		t.Fatal("bad json.Number")
	}
	if _, ok := asInt("3"); ok {
		t.Fatal("string should fail")
	}
	if _, ok := asInt(nil); ok {
		t.Fatal("nil should fail")
	}
}
