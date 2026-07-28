package compute_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/compute"
)

func TestMockInvokerStatusAndDelay(t *testing.T) {
	inv := compute.MockInvoker{}
	start := time.Now()
	res, err := inv.Invoke(context.Background(), compute.InvokeRequest{
		StatusCode:   503,
		Delay:        20 * time.Millisecond,
		ResponseBody: []byte(`{"err":"busy"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < 15*time.Millisecond {
		t.Fatal("expected delay theatre")
	}
	if res.StatusCode != 503 || string(res.Body) != `{"err":"busy"}` {
		t.Fatalf("result=%#v", res)
	}
}

func TestDockerInvokerRejectsHostSock(t *testing.T) {
	inv := compute.DockerInvoker{Host: "unix:///var/run/docker.sock"}
	_, err := inv.Invoke(context.Background(), compute.InvokeRequest{})
	if err == nil || !strings.Contains(err.Error(), "docker.sock") {
		t.Fatalf("want docker.sock reject, got %v", err)
	}
}

func TestNewInvokerFromEnvDefaultMock(t *testing.T) {
	t.Setenv(compute.EnvDockerHost, "")
	_ = os.Unsetenv(compute.EnvDockerHost)
	inv := compute.NewInvokerFromEnv()
	if _, ok := inv.(compute.MockInvoker); !ok {
		t.Fatalf("expected MockInvoker, got %T", inv)
	}
}

func TestNewInvokerFromEnvDockerStub(t *testing.T) {
	t.Setenv(compute.EnvDockerHost, "tcp://127.0.0.1:2376")
	t.Setenv(compute.EnvDockerHostAllowlist, "tcp://127.0.0.1:2376")
	inv := compute.NewInvokerFromEnv()
	d, ok := inv.(compute.DockerInvoker)
	if !ok {
		t.Fatalf("expected DockerInvoker, got %T", inv)
	}
	// Soft-fail: missing TLS PEMs / unreachable engine falls back to mock with detail.
	res, err := d.Invoke(context.Background(), compute.InvokeRequest{
		ServiceName:  "svc",
		ResponseBody: []byte(`{"ok":true}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("fallback status=%d", res.StatusCode)
	}
	if !strings.Contains(string(res.Body), `"mode":"mock"`) {
		t.Fatalf("expected soft-fail engine detail, body=%s", res.Body)
	}
}
