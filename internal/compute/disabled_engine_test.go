package compute

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDialDisabledAndValidateHost(t *testing.T) {
	c, err := Dial("", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Enabled() {
		t.Fatal("empty host must disable client")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDockerHost("", ""); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDockerHost("unix:///var/run/docker.sock", ""); err == nil {
		t.Fatal("unix sock must fail")
	}
	if err := ValidateDockerHost("tcp://noctaxris-gcp-engine:2376", ""); err == nil {
		t.Fatal("missing cert path must fail")
	}
	if err := ValidateDockerHost("http://evil.example", t.TempDir()); err == nil {
		t.Fatal("http scheme must fail")
	}
	dir := t.TempDir()
	for _, name := range []string{"ca.pem", "cert.pem", "key.pem"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(EnvDockerHostAllowlist, "tcp://custom-engine:2376")
	if err := ValidateDockerHost("tcp://custom-engine:2376", dir); err != nil {
		t.Fatalf("allowlisted host: %v", err)
	}
	if err := ValidateDockerHost("tcp://not-listed:2376", dir); err == nil {
		t.Fatal("non-allowlisted must fail")
	}
}

func TestDisabledClientEarlyReturns(t *testing.T) {
	c, err := Dial("", "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := c.Ping(ctx); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("Ping: %v", err)
	}
	if _, err := c.RunLabOneShot(ctx, "alpine:3.23"); err == nil {
		t.Fatal("RunLabOneShot expected disabled error")
	}
	if _, err := c.RunBuildStep(ctx, BuildStepRun{Image: "alpine:3.23"}); err == nil {
		t.Fatal("RunBuildStep expected disabled error")
	}
	if _, err := c.StartLabDaemon(ctx, "alpine:3.23", "name", nil, 5432); err == nil {
		t.Fatal("StartLabDaemon expected disabled error")
	}
	if err := c.RemoveLabDaemon(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveLabDaemon(ctx, "id"); err != nil {
		t.Fatal(err)
	}
	if err := c.ExecLabDaemon(ctx, "id", []string{"true"}); err == nil {
		t.Fatal("ExecLabDaemon expected disabled error")
	}
	if _, _, err := c.EnsureRedpanda(ctx, "rp", RedpandaOwner{Project: "p", Location: "l", ClusterID: "c"}); err == nil {
		t.Fatal("EnsureRedpanda expected disabled error")
	}
	if err := c.RemoveRedpanda(ctx, "id"); err != nil {
		t.Fatal(err)
	}
	if err := c.CreateRedpandaTopic(ctx, "id", "topic", 1, 1); err == nil {
		t.Fatal("CreateRedpandaTopic expected disabled error")
	}
	if _, err := c.EnsureMemorystoreRedis(ctx, "inst", ""); err == nil {
		t.Fatal("EnsureMemorystoreRedis expected disabled error")
	}
	if err := c.RemoveMemorystoreRedis(ctx, "inst", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := c.StartRunHTTP(ctx, "alpine:3.23", "n", nil, 8080); err == nil {
		t.Fatal("StartRunHTTP expected disabled error")
	}
	if _, err := c.StartRunHTTPWithOptions(ctx, RunHTTPOptions{Image: "alpine:3.23", Name: "n"}); err == nil {
		t.Fatal("StartRunHTTPWithOptions expected disabled error")
	}
	if err := c.RemoveRunHTTP(ctx, ""); err != nil {
		t.Fatal(err)
	}
}

func TestMemorystoreRedisHelpersAndEnvNoop(t *testing.T) {
	t.Setenv(EnvDockerHost, "")
	ctx := context.Background()
	if _, err := EnsureMemorystoreRedisFromEnv(ctx, "inst", "pw"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveMemorystoreRedisFromEnv(ctx, "inst", ""); err != nil {
		t.Fatal(err)
	}
	if got := MemorystoreRedisContainerName(""); got != "noctaxris-gcp-redis-lab" {
		t.Fatalf("empty name=%q", got)
	}
	if got := MemorystoreRedisContainerName("A_B/C"); !strings.HasPrefix(got, "noctaxris-gcp-redis-") {
		t.Fatalf("sanitized=%q", got)
	}
	long := strings.Repeat("a", 80)
	if got := MemorystoreRedisContainerName(long); len(got) > 63 {
		t.Fatalf("len=%d name=%q", len(got), got)
	}
	if MemorystoreRedisAuthEnv("") != nil {
		t.Fatal("empty auth env")
	}
	if got := MemorystoreRedisAuthEnv("pw"); len(got) != 1 || got[0] != "REDIS_PASSWORD=pw" {
		t.Fatalf("auth env=%v", got)
	}
	if MemorystoreRedisAuthCmd("") != nil {
		t.Fatal("empty auth cmd")
	}
	if got := MemorystoreRedisAuthCmd("pw"); len(got) != 3 {
		t.Fatalf("auth cmd=%v", got)
	}
}

func TestRunHTTPHelpersAndImageAllow(t *testing.T) {
	if name := RunHTTPContainerName("", "", ""); !strings.HasPrefix(name, "noctaxris-gcp-run-http-") {
		t.Fatalf("name=%q", name)
	}
	if name := RunHTTPContainerName("p", "l", "s"); !strings.HasPrefix(name, "noctaxris-gcp-run-http-") {
		t.Fatalf("name=%q", name)
	}
	if _, err := EngineHostFromDockerHost(""); err == nil {
		t.Fatal("empty host")
	}
	if _, err := EngineHostFromDockerHost("unix:///x"); err == nil {
		t.Fatal("unix scheme")
	}
	host, err := EngineHostFromDockerHost("tcp://noctaxris-gcp-engine:2376")
	if err != nil || host != "noctaxris-gcp-engine" {
		t.Fatalf("host=%q err=%v", host, err)
	}
	t.Setenv(EnvRunPublishPort, "")
	if n, err := RunPublishPortFromEnv(); err != nil || n != 0 {
		t.Fatalf("ephemeral: %d %v", n, err)
	}
	t.Setenv(EnvRunPublishPort, "8089")
	if n, err := RunPublishPortFromEnv(); err != nil || n != 8089 {
		t.Fatalf("fixed: %d %v", n, err)
	}
	t.Setenv(EnvRunPublishPort, "99999")
	if _, err := RunPublishPortFromEnv(); err == nil {
		t.Fatal("out of range")
	}
	if err := AllowImagePull(""); err == nil {
		t.Fatal("empty image")
	}
	if err := AllowImagePull("alpine:3.23"); err != nil {
		t.Fatal(err)
	}
	if err := AllowImagePull("evil/image:latest"); err == nil {
		t.Fatal("unlisted image")
	}
	t.Setenv(EnvImagePullAllowlist, "gcr.io/demo/,plainlabimage")
	if err := AllowImagePull("plainlabimage"); err != nil {
		t.Fatal(err)
	}
	if err := AllowImagePull("gcr.io/demo/app:1"); err == nil {
		t.Fatal("prefix without digest must fail")
	}
	if err := AllowImagePull("gcr.io/demo/app@sha256:" + strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
}

func TestDockerInvokerFailPathsAndMock(t *testing.T) {
	ctx := context.Background()
	if _, err := (DockerInvoker{}).Invoke(ctx, InvokeRequest{}); err == nil {
		t.Fatal("empty host")
	}
	if _, err := (DockerInvoker{Host: "unix:///var/run/docker.sock"}).Invoke(ctx, InvokeRequest{}); err == nil {
		t.Fatal("docker.sock")
	}
	res, err := (MockInvoker{}).Invoke(ctx, InvokeRequest{StatusCode: 201, ResponseBody: []byte(`{"a":1}`)})
	if err != nil || res.StatusCode != 201 {
		t.Fatalf("mock: %#v err=%v", res, err)
	}
	cancel, cancelFn := context.WithCancel(ctx)
	cancelFn()
	if _, err := (MockInvoker{}).Invoke(cancel, InvokeRequest{Delay: time.Hour}); err == nil {
		t.Fatal("cancelled delay")
	}
	inv := NewInvoker("", "")
	if _, ok := inv.(MockInvoker); !ok {
		t.Fatalf("expected MockInvoker, got %T", inv)
	}
	t.Setenv(EnvNestedInvokeFailClosed, "true")
	t.Setenv(EnvDockerHost, "")
	_ = NewInvokerFromEnv()
	out := mergeEngineDetail([]byte("not-json"), "mock", "detail")
	if !strings.Contains(string(out), `"mode":"mock"`) {
		t.Fatalf("merge non-json: %s", out)
	}
	out = mergeEngineDetail([]byte(`{"ok":true}`), "mock", "dial failed")
	if !strings.Contains(string(out), "dial failed") {
		t.Fatalf("merge json: %s", out)
	}
	d := DockerInvoker{Host: "tcp://noctaxris-gcp-engine:2376", TLSCertDir: t.TempDir(), FailClosed: true}
	if _, err := d.Invoke(ctx, InvokeRequest{}); err == nil {
		t.Fatal("fail-closed dial should error")
	}
	d.FailClosed = false
	res, err = d.Invoke(ctx, InvokeRequest{ResponseBody: []byte(`{"ok":true}`)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res.Body), "engine") {
		t.Fatalf("soft-fail body=%s", res.Body)
	}
}

func TestRedpandaNamesAndSysctls(t *testing.T) {
	name := RedpandaContainerNameForCluster("p", "l", "c")
	if !strings.HasPrefix(name, "noctaxris-gcp-kafka-") {
		t.Fatalf("name=%q", name)
	}
	if cmd := RedpandaStartCmd("broker"); len(cmd) == 0 {
		t.Fatal("empty redpanda cmd")
	}
	if sys := LabNetSysctls(); len(sys) == 0 {
		t.Fatal("empty sysctls")
	}
	t.Setenv(EnvInjectHostGateway, "")
	if HostGatewayExtraHosts() != nil {
		t.Fatal("host gateway default off")
	}
	t.Setenv(EnvInjectHostGateway, "1")
	if len(HostGatewayExtraHosts()) != 1 {
		t.Fatal("host gateway opt-in")
	}
	t.Setenv(EnvNestedEngineFailClosed, "")
	if NestedEngineFailClosed() {
		t.Fatal("fail-closed default off")
	}
	if msg := NestedEngineFailClosedMessage(nil); msg == "" {
		t.Fatal("empty message")
	}
}
