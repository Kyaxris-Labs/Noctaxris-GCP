package compute

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	// EnvDockerHostAllowlist extends the default Docker host allowlist (comma-separated exact URLs).
	EnvDockerHostAllowlist = "NOCTAXRIS_GCP_DOCKER_HOST_ALLOWLIST"

	defaultDockerHost = "tcp://noctaxris-gcp-engine:2376"
)

// ValidateDockerHost rejects host-engine schemes and non-allowlisted endpoints.
// Empty dockerHost is valid (compute disabled). When non-empty, tlsCertPath must
// point at a directory with readable ca.pem, cert.pem, and key.pem.
func ValidateDockerHost(dockerHost, tlsCertPath string) error {
	host := strings.TrimSpace(dockerHost)
	if host == "" {
		return nil
	}
	lower := strings.ToLower(host)
	if strings.Contains(lower, "docker.sock") {
		return fmt.Errorf("compute: NOCTAXRIS_GCP_DOCKER_HOST must not reference docker.sock")
	}
	u, err := url.Parse(host)
	if err != nil {
		return fmt.Errorf("compute: NOCTAXRIS_GCP_DOCKER_HOST: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	switch scheme {
	case "unix", "npipe":
		return fmt.Errorf("compute: NOCTAXRIS_GCP_DOCKER_HOST scheme %q is not allowed (use tcp:// to noctaxris-gcp-engine)", scheme)
	case "tcp":
		// ok
	default:
		if scheme == "" {
			return fmt.Errorf("compute: NOCTAXRIS_GCP_DOCKER_HOST must be a tcp:// URL")
		}
		return fmt.Errorf("compute: NOCTAXRIS_GCP_DOCKER_HOST scheme %q is not allowed", scheme)
	}
	if !dockerHostAllowlisted(host) {
		return fmt.Errorf("compute: NOCTAXRIS_GCP_DOCKER_HOST %q is not allowlisted (default %s; extend via %s)",
			host, defaultDockerHost, EnvDockerHostAllowlist)
	}
	certDir := strings.TrimSpace(tlsCertPath)
	if certDir == "" {
		return fmt.Errorf("compute: NOCTAXRIS_GCP_DOCKER_CERT_PATH is required when NOCTAXRIS_GCP_DOCKER_HOST is set")
	}
	for _, name := range []string{"ca.pem", "cert.pem", "key.pem"} {
		p := filepath.Join(certDir, name)
		st, err := os.Stat(p)
		if err != nil {
			return fmt.Errorf("compute: NOCTAXRIS_GCP_DOCKER_CERT_PATH: %s: %w", name, err)
		}
		if st.IsDir() || st.Size() == 0 {
			return fmt.Errorf("compute: NOCTAXRIS_GCP_DOCKER_CERT_PATH: %s must be a non-empty file", name)
		}
	}
	return nil
}

func dockerHostAllowlisted(host string) bool {
	want := strings.TrimRight(strings.TrimSpace(host), "/")
	for _, entry := range dockerHostAllowlist() {
		if strings.EqualFold(entry, want) {
			return true
		}
	}
	return false
}

func dockerHostAllowlist() []string {
	out := []string{defaultDockerHost}
	raw := strings.TrimSpace(os.Getenv(EnvDockerHostAllowlist))
	if raw == "" {
		return out
	}
	for _, part := range strings.Split(raw, ",") {
		entry := strings.TrimRight(strings.TrimSpace(part), "/")
		if entry != "" {
			out = append(out, entry)
		}
	}
	return out
}
