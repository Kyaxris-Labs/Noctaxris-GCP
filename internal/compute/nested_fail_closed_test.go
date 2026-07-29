package compute_test

import (
	"errors"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/compute"
)

func TestNestedEngineFailClosedFromEnv(t *testing.T) {
	_ = unsetNestedEngineFailClosed(t)
	if compute.NestedEngineFailClosed() {
		t.Fatal("expected fail-closed off by default")
	}
	t.Setenv(compute.EnvNestedEngineFailClosed, "1")
	if !compute.NestedEngineFailClosed() {
		t.Fatal("expected fail-closed for 1")
	}
	t.Setenv(compute.EnvNestedEngineFailClosed, "true")
	if !compute.NestedEngineFailClosed() {
		t.Fatal("expected fail-closed for true")
	}
	t.Setenv(compute.EnvNestedEngineFailClosed, "0")
	if compute.NestedEngineFailClosed() {
		t.Fatal("expected fail-closed off for 0")
	}
}

func TestNestedEngineFailClosedMessage(t *testing.T) {
	if got := compute.NestedEngineFailClosedMessage(nil); got != "nested engine start failed" {
		t.Fatalf("nil: %q", got)
	}
	got := compute.NestedEngineFailClosedMessage(errors.New("dial refused"))
	if got != "nested engine start failed: dial refused" {
		t.Fatalf("err: %q", got)
	}
}

func unsetNestedEngineFailClosed(t *testing.T) error {
	t.Helper()
	t.Setenv(compute.EnvNestedEngineFailClosed, "")
	return nil
}
