package compute

import (
	"fmt"
	"os"
	"strings"
)

// EnvImagePullAllowlist extends the default image pull allowlist with comma-separated
// exact image refs, or registry prefixes that end with "/" (digest required).
const EnvImagePullAllowlist = "NOCTAXRIS_GCP_IMAGE_PULL_ALLOWLIST"

// DefaultLabImage is the pinned alpine base used for Cloud Run nested invoke scaffolding.
const DefaultLabImage = "alpine:3.20"

// AllowImagePull fails closed unless imageRef is a pinned lab base image or an
// explicit allowlist entry. Prefix entries must end with "/" and the ref must
// include @sha256:... — bare substring prefixes are rejected.
func AllowImagePull(imageRef string) error {
	ref := strings.TrimSpace(imageRef)
	if ref == "" {
		return fmt.Errorf("compute: image reference is empty")
	}
	if isPinnedLabImage(ref) {
		return nil
	}
	for _, entry := range extraImageAllowEntries() {
		if entry == "" {
			continue
		}
		if strings.HasSuffix(entry, "/") {
			if !strings.HasPrefix(ref, entry) {
				continue
			}
			if !strings.Contains(ref, "@sha256:") {
				return fmt.Errorf("compute: allowlisted image %q must be pinned by digest (@sha256:...)", ref)
			}
			if imageAllowEntryNeedsDigest(entry) {
				return nil
			}
			return nil
		}
		if ref == entry {
			if imageAllowEntryNeedsDigest(entry) && !strings.Contains(ref, "@sha256:") {
				return fmt.Errorf("compute: allowlisted image %q must be pinned by digest (@sha256:...)", ref)
			}
			return nil
		}
	}
	return fmt.Errorf("compute: image pull host not allowlisted: %q (pinned lab bases only)", ref)
}

func isPinnedLabImage(ref string) bool {
	lower := strings.ToLower(ref)
	pinnedExact := map[string]struct{}{
		"alpine:3.20":                               {},
		"public.ecr.aws/docker/library/alpine:3.20": {},
		"gcr.io/google-containers/pause:3.9":        {},
		"rancher/k3s:v1.28.8-k3s1":                  {},
		"postgres:16-alpine":                        {},
		"mysql:8.0":                                 {},
		"redis:7-alpine":                            {},
		"docker.redpanda.com/redpandadata/redpanda:v24.2.4": {},
	}
	_, ok := pinnedExact[lower]
	return ok
}

func imageAllowEntryNeedsDigest(entry string) bool {
	host := entry
	if i := strings.IndexAny(entry, "/@"); i >= 0 {
		host = entry[:i]
	}
	return strings.Contains(host, ".") || strings.Contains(host, ":")
}

func extraImageAllowEntries() []string {
	raw := strings.TrimSpace(os.Getenv(EnvImagePullAllowlist))
	if raw == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		p := strings.TrimSpace(part)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
