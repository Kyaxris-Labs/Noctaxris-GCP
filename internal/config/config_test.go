package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/config"
)

func TestLoadFromEnvDefaults(t *testing.T) {
	t.Setenv("NOCTAXRIS_GCP_LISTEN", "")
	t.Setenv("NOCTAXRIS_GCP_DATA_ROOT", "")
	t.Setenv("NOCTAXRIS_GCP_PROJECT", "")
	t.Setenv("NOCTAXRIS_GCP_DOCKER_HOST", "")
	t.Setenv("NOCTAXRIS_GCP_DOCKER_CERT_PATH", "")
	t.Setenv(config.EnvAllowNonLoopbackListen, "")

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != config.DefaultListenAddr {
		t.Fatalf("ListenAddr = %q, want %q", cfg.ListenAddr, config.DefaultListenAddr)
	}
	if cfg.DataRoot != config.DefaultDataRoot {
		t.Fatalf("DataRoot = %q, want %q", cfg.DataRoot, config.DefaultDataRoot)
	}
	if cfg.ProjectID != config.DefaultProjectID {
		t.Fatalf("ProjectID = %q, want %q", cfg.ProjectID, config.DefaultProjectID)
	}
	if cfg.DockerHost != "" {
		t.Fatalf("DockerHost = %q, want empty (compute disabled)", cfg.DockerHost)
	}
}

func TestLoadFromEnvDockerHost(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"ca.pem", "cert.pem", "key.pem"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("lab-pem"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("NOCTAXRIS_GCP_LISTEN", "127.0.0.1:4588")
	t.Setenv("NOCTAXRIS_GCP_DOCKER_HOST", "tcp://noctaxris-gcp-engine:2376")
	t.Setenv("NOCTAXRIS_GCP_DOCKER_CERT_PATH", dir)

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DockerHost != "tcp://noctaxris-gcp-engine:2376" {
		t.Fatalf("DockerHost = %q", cfg.DockerHost)
	}
	if cfg.DockerTLSCertPath != dir {
		t.Fatalf("DockerTLSCertPath = %q", cfg.DockerTLSCertPath)
	}
}

func TestLoadFromEnvDockerHostRejectsSock(t *testing.T) {
	t.Setenv("NOCTAXRIS_GCP_DOCKER_HOST", "unix:///var/run/docker.sock")
	t.Setenv("NOCTAXRIS_GCP_DOCKER_CERT_PATH", t.TempDir())
	if _, err := config.LoadFromEnv(); err == nil {
		t.Fatal("expected reject for docker.sock")
	}
}

func TestLoadFromEnvDockerHostRequiresCertPath(t *testing.T) {
	t.Setenv("NOCTAXRIS_GCP_DOCKER_HOST", "tcp://noctaxris-gcp-engine:2376")
	t.Setenv("NOCTAXRIS_GCP_DOCKER_CERT_PATH", "")
	if _, err := config.LoadFromEnv(); err == nil {
		t.Fatal("expected reject when cert path empty")
	}
}

func TestLoadFromEnvOverrides(t *testing.T) {
	t.Setenv("NOCTAXRIS_GCP_LISTEN", "127.0.0.1:4599")
	t.Setenv("NOCTAXRIS_GCP_DATA_ROOT", "/tmp/ngcp-data")
	t.Setenv("NOCTAXRIS_GCP_PROJECT", "lab-project")
	t.Setenv("NOCTAXRIS_GCP_ROOT_SERVICE_ACCOUNT", "sa@lab.iam.gserviceaccount.com")
	t.Setenv("NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN", "lab-token")

	cfg, err := config.LoadFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != "127.0.0.1:4599" {
		t.Fatalf("ListenAddr = %q", cfg.ListenAddr)
	}
	if cfg.DataRoot != "/tmp/ngcp-data" {
		t.Fatalf("DataRoot = %q", cfg.DataRoot)
	}
	if cfg.ProjectID != "lab-project" {
		t.Fatalf("ProjectID = %q", cfg.ProjectID)
	}
	if cfg.RootServiceAccount != "sa@lab.iam.gserviceaccount.com" {
		t.Fatalf("RootServiceAccount = %q", cfg.RootServiceAccount)
	}
	if cfg.RootAccessToken != "lab-token" {
		t.Fatalf("RootAccessToken = %q", cfg.RootAccessToken)
	}
}

func TestListenIsLoopback(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:4588", true},
		{"localhost:4588", true},
		{"[::1]:4588", true},
		{":4588", false},
		{"0.0.0.0:4588", false},
		{"[::]:4588", false},
		{"192.168.1.10:4588", false},
	}
	for _, tc := range cases {
		if got := config.ListenIsLoopback(tc.addr); got != tc.want {
			t.Fatalf("ListenIsLoopback(%q)=%v want %v", tc.addr, got, tc.want)
		}
	}
}

func TestValidateListenSecurityNonLoopbackRequiresTLSOrOptIn(t *testing.T) {
	t.Setenv(config.EnvAllowNonLoopbackListen, "")
	cfg := config.Config{ListenAddr: "0.0.0.0:4588"}
	if err := config.ValidateListenSecurity(cfg); err == nil {
		t.Fatal("expected error for non-loopback without TLS")
	}
	cfg.AllowNonLoopbackListen = true
	if err := config.ValidateListenSecurity(cfg); err != nil {
		t.Fatalf("opt-in should allow: %v", err)
	}
}

func TestExampleRootCredentials(t *testing.T) {
	if !config.ExampleRootCredentials("root@example.iam.gserviceaccount.com", "noctaxris-gcp-example-root-token") {
		t.Fatal("shipped example pair must match")
	}
	if config.ExampleRootCredentials("root@example.iam.gserviceaccount.com", "other") {
		t.Fatal("mismatched token must not match")
	}
}
