package securitycenter

import (
	"os"
	"testing"
)

func TestInjectEnabledFromEnv(t *testing.T) {
	t.Setenv(EnvSCCInject, "")
	if InjectEnabledFromEnv() {
		t.Fatal("empty should be false")
	}
	t.Setenv(EnvSCCInject, "1")
	if !InjectEnabledFromEnv() {
		t.Fatal("1")
	}
	t.Setenv(EnvSCCInject, "true")
	if !InjectEnabledFromEnv() {
		t.Fatal("true")
	}
	t.Setenv(EnvSCCInject, "no")
	if InjectEnabledFromEnv() {
		t.Fatal("no")
	}
	_ = os.Unsetenv(EnvSCCInject)
}
