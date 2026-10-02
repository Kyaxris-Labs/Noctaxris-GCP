package compute

import "testing"

func TestLabNetSysctls(t *testing.T) {
	t.Parallel()
	got := LabNetSysctls()
	if got["net.ipv6.conf.all.disable_ipv6"] != "1" {
		t.Fatalf("all = %#v", got)
	}
	if got["net.ipv6.conf.default.disable_ipv6"] != "1" {
		t.Fatalf("default = %#v", got)
	}
}
