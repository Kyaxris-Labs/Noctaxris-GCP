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
	b, err := os.ReadFile("compose.yaml")
	if err != nil {
		t.Fatal(err)
	}
	content := string(b)
	if strings.Contains(content, "/var/run/docker.sock") {
		t.Fatal("compose must not mount /var/run/docker.sock")
	}
	if hasDockerSockVolumeEntry(content) {
		t.Fatal("compose must not bind or volume-mount docker.sock")
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
