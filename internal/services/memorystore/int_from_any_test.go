package memorystore

import (
	"encoding/json"
	"testing"
)

func TestIntFromAnyAndBoolFromAny(t *testing.T) {
	if intFromAny(float64(3.9)) != 3 {
		t.Fatal("float64")
	}
	if intFromAny(7) != 7 {
		t.Fatal("int")
	}
	if intFromAny(int64(11)) != 11 {
		t.Fatal("int64")
	}
	if intFromAny(json.Number("42")) != 42 {
		t.Fatal("json.Number")
	}
	if intFromAny("nope") != 0 {
		t.Fatal("default")
	}
	if !boolFromAny(true) || boolFromAny(false) {
		t.Fatal("bool")
	}
	if boolFromAny("x") {
		t.Fatal("default bool")
	}
}
