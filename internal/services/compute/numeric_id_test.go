package compute

import "testing"

func TestStableGCEInstanceNumericID(t *testing.T) {
	a := stableGCEInstanceNumericID("projects/p/zones/z/instances/i1")
	b := stableGCEInstanceNumericID("projects/p/zones/z/instances/i1")
	c := stableGCEInstanceNumericID("projects/p/zones/z/instances/i2")
	if a == "" || a != b {
		t.Fatalf("stable a=%q b=%q", a, b)
	}
	if a == c {
		t.Fatal("different names should differ")
	}
}
