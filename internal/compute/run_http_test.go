package compute

import (
	"context"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/network"
)

func TestEngineHostFromDockerHost(t *testing.T) {
	t.Parallel()
	got, err := EngineHostFromDockerHost("tcp://noctaxris-gcp-engine:2376")
	if err != nil || got != "noctaxris-gcp-engine" {
		t.Fatalf("got %q err=%v", got, err)
	}
	for _, bad := range []string{"", "unix:///var/run/docker.sock", "tcp://:2376"} {
		if _, err := EngineHostFromDockerHost(bad); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}

func TestRunPublishPortFromEnv(t *testing.T) {
	t.Setenv(EnvRunPublishPort, "")
	if n, err := RunPublishPortFromEnv(); err != nil || n != 0 {
		t.Fatalf("unset: n=%d err=%v", n, err)
	}
	t.Setenv(EnvRunPublishPort, "8091")
	if n, err := RunPublishPortFromEnv(); err != nil || n != 8091 {
		t.Fatalf("fixed: n=%d err=%v", n, err)
	}
	for _, bad := range []string{"0", "70000", "abc", "-1"} {
		t.Setenv(EnvRunPublishPort, bad)
		if _, err := RunPublishPortFromEnv(); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}

func TestRunPortBindings(t *testing.T) {
	t.Parallel()
	port := network.MustParsePort("8080/tcp")
	eph := runPortBindings(port, 0)
	if len(eph[port]) != 1 || eph[port][0].HostPort != "" {
		t.Fatalf("ephemeral=%#v", eph)
	}
	fixed := runPortBindings(port, 8091)
	if len(fixed[port]) != 1 || fixed[port][0].HostPort != "8091" {
		t.Fatalf("fixed=%#v", fixed)
	}
}

func TestRunHostConfigHostGateway(t *testing.T) {
	port := network.MustParsePort("8080/tcp")
	t.Setenv(EnvInjectHostGateway, "")
	if cfg := runHostConfig(port, 0); len(cfg.ExtraHosts) != 0 {
		t.Fatalf("default ExtraHosts = %#v want none", cfg.ExtraHosts)
	}
	t.Setenv(EnvInjectHostGateway, "1")
	cfg := runHostConfig(port, 8091)
	if len(cfg.ExtraHosts) != 1 || cfg.ExtraHosts[0] != "host.docker.internal:host-gateway" {
		t.Fatalf("opt-in ExtraHosts = %#v", cfg.ExtraHosts)
	}
	if string(cfg.NetworkMode) != LabDaemonNetwork {
		t.Fatalf("network = %q", cfg.NetworkMode)
	}
}

func TestHostPortFromPortMap(t *testing.T) {
	t.Parallel()
	port := network.MustParsePort("8080/tcp")
	got, err := hostPortFromPortMap(network.PortMap{port: {{HostPort: "49153"}}}, 8080)
	if err != nil || got != 49153 {
		t.Fatalf("got %d err=%v", got, err)
	}
	if _, err := hostPortFromPortMap(network.PortMap{}, 8080); err == nil {
		t.Fatal("expected error when no binding")
	}
	if _, err := hostPortFromPortMap(network.PortMap{port: {{HostPort: ""}}}, 8080); err == nil {
		t.Fatal("expected error for empty host port")
	}
}

func TestRunHTTPContainerNameStableAndScoped(t *testing.T) {
	t.Parallel()
	a := RunHTTPContainerName("p", "us-central1", "svc")
	if a != RunHTTPContainerName("p", "us-central1", "svc") {
		t.Fatal("name must be stable")
	}
	if a == RunHTTPContainerName("p2", "us-central1", "svc") || a == RunHTTPContainerName("p", "us-east1", "svc") {
		t.Fatal("name must be scoped to project and location")
	}
	if !strings.HasPrefix(a, "noctaxris-gcp-run-http-") {
		t.Fatalf("name=%q", a)
	}
}

func TestStartRunHTTPDisabled(t *testing.T) {
	c, err := Dial("", "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := c.StartRunHTTP(ctx, "python:3.13-slim-bookworm", "n", nil, 8080); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("disabled: %v", err)
	}
	if err := c.RemoveRunHTTP(ctx, "id"); err != nil {
		t.Fatalf("remove disabled is no-op: %v", err)
	}
}
