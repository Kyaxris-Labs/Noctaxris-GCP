package compute

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestDisabledClientFailClosedPaths(t *testing.T) {
	c, err := Dial("", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if c.Enabled() {
		t.Fatal("empty host should disable client")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.Ping(ctx); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("ping: %v", err)
	}
	if _, err := c.RunLabOneShot(ctx, ""); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("oneshot: %v", err)
	}
	if _, err := c.StartLabDaemon(ctx, DefaultLabImage, "name", nil, 0); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("daemon: %v", err)
	}
	if err := c.RemoveLabDaemon(ctx, "name"); err != nil {
		t.Fatalf("remove daemon disabled is no-op: %v", err)
	}
	if err := c.ExecLabDaemon(ctx, "name", []string{"true"}); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("exec: %v", err)
	}
	if _, _, err := c.EnsureRedpanda(ctx, "cluster"); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("redpanda: %v", err)
	}
	if err := c.RemoveRedpanda(ctx, "cluster"); err != nil {
		t.Fatalf("remove redpanda disabled is no-op: %v", err)
	}
	if err := c.CreateRedpandaTopic(ctx, "cluster", "topic", 1, 1); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("topic: %v", err)
	}
	if _, err := c.EnsureMemorystoreRedis(ctx, "inst", ""); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("memorystore: %v", err)
	}
	if err := c.RemoveMemorystoreRedis(ctx, "inst", ""); err != nil {
		t.Fatalf("remove memorystore disabled is no-op: %v", err)
	}
}

func TestRemoveMemorystoreRedisFromEnvNoHost(t *testing.T) {
	t.Setenv(EnvDockerHost, "")
	if err := RemoveMemorystoreRedisFromEnv(context.Background(), "i", ""); err != nil {
		t.Fatal(err)
	}
}
