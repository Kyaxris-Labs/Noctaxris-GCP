package docker_test

import (
	"os"
	"strings"
	"testing"
)

func TestComposePublishesLocalhost4588(t *testing.T) {
	b, err := os.ReadFile("compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	content := string(b)
	if !strings.Contains(content, `"127.0.0.1:4588:4588"`) {
		t.Fatal("compose must publish 127.0.0.1:4588:4588")
	}
	if strings.Contains(content, `"0.0.0.0:4588:4588"`) || strings.Contains(content, `- "4588:4588"`) {
		t.Fatal("compose must not hardcode non-loopback host publish for 4588")
	}
}

func TestComposeFileHasNoDockerSock(t *testing.T) {
	for _, name := range []string{"compose.yaml", "compose.engine.yaml", "compose.engine-privileged.yaml"} {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		content := string(b)
		if strings.Contains(content, "/var/run/docker.sock") {
			t.Fatalf("%s must not mount /var/run/docker.sock", name)
		}
		if hasDockerSockVolumeEntry(content) {
			t.Fatalf("%s must not bind or volume-mount docker.sock", name)
		}
	}
}

func TestComposeEngineOverlayOptIn(t *testing.T) {
	base, err := os.ReadFile("compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(base), "NOCTAXRIS_GCP_DOCKER_HOST") {
		t.Fatal("default compose.yaml must leave DOCKER_HOST unset (engine opt-in only)")
	}
	if strings.Contains(string(base), "noctaxris-gcp-engine") {
		t.Fatal("default compose.yaml must not define noctaxris-gcp-engine")
	}

	overlay, err := os.ReadFile("compose.engine.yaml")
	if err != nil {
		t.Fatal("compose.engine.yaml must exist:", err)
	}
	content := string(overlay)
	if !strings.Contains(content, "NOCTAXRIS_GCP_DOCKER_HOST") {
		t.Fatal("engine overlay must set NOCTAXRIS_GCP_DOCKER_HOST")
	}
	if !strings.Contains(content, "tcp://noctaxris-gcp-engine:2376") {
		t.Fatal("engine overlay must point at noctaxris-gcp-engine:2376")
	}
	if !strings.Contains(content, "NOCTAXRIS_GCP_DOCKER_CERT_PATH") {
		t.Fatal("engine overlay must set NOCTAXRIS_GCP_DOCKER_CERT_PATH")
	}
	if !strings.Contains(content, "privileged: false") {
		t.Fatal("engine overlay must use restricted DinD (privileged: false)")
	}
	if strings.Contains(content, "2376:2376") || strings.Contains(content, `"2376:`) {
		t.Fatal("engine API must not be published to the host")
	}

	priv, err := os.ReadFile("compose.engine-privileged.yaml")
	if err != nil {
		t.Fatal("compose.engine-privileged.yaml must exist:", err)
	}
	if !strings.Contains(string(priv), "privileged: true") {
		t.Fatal("privileged overlay must set privileged: true")
	}
}

// hasDockerSockVolumeEntry reports a non-comment YAML volume list item that
// mounts a path ending in docker.sock or uses a docker.sock: host bind.
// English comments such as "# DO NOT add docker.sock" are allowed.
func hasDockerSockVolumeEntry(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if idx := strings.Index(trimmed, " #"); idx >= 0 {
			trimmed = strings.TrimSpace(trimmed[:idx])
		}
		if !strings.HasPrefix(trimmed, "-") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
		if strings.HasSuffix(rest, "docker.sock") || strings.Contains(rest, "docker.sock:") {
			return true
		}
	}
	return false
}
