package iam

import (
	"encoding/json"
	"testing"
)

func TestJSONNumberAsInt64(t *testing.T) {
	if n, ok := jsonNumberAsInt64(float64(42)); !ok || n != 42 {
		t.Fatal("float64")
	}
	if n, ok := jsonNumberAsInt64(json.Number("7")); !ok || n != 7 {
		t.Fatal("json.Number")
	}
	if n, ok := jsonNumberAsInt64(int64(3)); !ok || n != 3 {
		t.Fatal("int64")
	}
	if n, ok := jsonNumberAsInt64(int(5)); !ok || n != 5 {
		t.Fatal("int")
	}
	if _, ok := jsonNumberAsInt64("nope"); ok {
		t.Fatal("string")
	}
}
