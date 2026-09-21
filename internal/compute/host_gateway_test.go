package compute

import "testing"

func TestHostGatewayExtraHosts(t *testing.T) {
	t.Setenv(EnvInjectHostGateway, "")
	if got := HostGatewayExtraHosts(); got != nil {
		t.Fatalf("default ExtraHosts = %#v want nil", got)
	}
	t.Setenv(EnvInjectHostGateway, "0")
	if got := HostGatewayExtraHosts(); got != nil {
		t.Fatalf("disabled ExtraHosts = %#v want nil", got)
	}
	t.Setenv(EnvInjectHostGateway, "1")
	got := HostGatewayExtraHosts()
	if len(got) != 1 || got[0] != "host.docker.internal:host-gateway" {
		t.Fatalf("opt-in ExtraHosts = %#v", got)
	}
	t.Setenv(EnvInjectHostGateway, "true")
	got = HostGatewayExtraHosts()
	if len(got) != 1 || got[0] != "host.docker.internal:host-gateway" {
		t.Fatalf("true ExtraHosts = %#v", got)
	}
}
