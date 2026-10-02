package compute

import "testing"

func TestProbePortAndPortInSpec(t *testing.T) {
	if probePort(float64(22)) != 22 {
		t.Fatal("float64")
	}
	if probePort(80) != 80 {
		t.Fatal("int")
	}
	if probePort(int64(443)) != 443 {
		t.Fatal("int64")
	}
	if probePort("8080") != 8080 {
		t.Fatal("string")
	}
	if probePort(struct{}{}) != 0 {
		t.Fatal("default")
	}
	if portInSpec("", 80) {
		t.Fatal("empty spec")
	}
	if !portInSpec("80", 80) {
		t.Fatal("exact")
	}
	if !portInSpec("80-90", 85) {
		t.Fatal("range in")
	}
	if portInSpec("80-90", 91) {
		t.Fatal("range out")
	}
	if !cidrMatches("0.0.0.0/0", "1.2.3.4") {
		t.Fatal("any")
	}
	if !cidrMatches("10.0.0.0/8", "10.1.2.3") {
		t.Fatal("cidr match")
	}
	if cidrMatches("10.0.0.0/8", "11.0.0.1") {
		t.Fatal("cidr miss")
	}
	if cidrMatches("", "1.1.1.1") {
		t.Fatal("empty cidr")
	}
	if !sourceRangeMatches(map[string]any{}, "1.1.1.1") {
		t.Fatal("default open when ranges omitted")
	}
	if sourceRangeMatches(map[string]any{"sourceRanges": []any{"10.0.0.0/8"}}, "") {
		t.Fatal("empty src with ranges")
	}
	disks := []any{
		map[string]any{"boot": false, "deviceName": "d1"},
		map[string]any{"boot": true, "deviceName": "boot"},
	}
	if m := firstBootDisk(disks); m["deviceName"] != "boot" {
		t.Fatalf("firstBootDisk=%#v", m)
	}
	if m := firstBootDisk([]any{map[string]any{"deviceName": "only"}}); m["deviceName"] != "only" {
		t.Fatalf("fallback=%#v", m)
	}
	if firstBootDisk(nil) != nil {
		t.Fatal("nil disks")
	}
	cloned := cloneMap(map[string]any{"a": 1})
	if cloned["a"].(float64) != 1 {
		t.Fatalf("clone=%#v", cloned)
	}
	if len(cloneMap(nil)) != 0 {
		t.Fatal("nil clone")
	}
}
