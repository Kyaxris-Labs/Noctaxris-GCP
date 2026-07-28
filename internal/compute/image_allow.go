package compute

import (
	"fmt"
	"os"
	"strings"
)

// EnvImagePullAllowlist extends the default image pull allowlist with comma-separated prefixes.
const EnvImagePullAllowlist = "NOCTAXRIS_GCP_IMAGE_PULL_ALLOWLIST"

// DefaultLabImage is the pinned alpine base used for Cloud Run nested invoke scaffolding.
const DefaultLabImage = "alpine:3.20"

// AllowImagePull fails closed unless imageRef is a pinned lab base image or an
// explicit allowlist prefix. Attacker-controlled registry hosts are rejected.
func AllowImagePull(imageRef string) error {
	ref := strings.TrimSpace(imageRef)
	if ref == "" {
		return fmt.Errorf("compute: image reference is empty")
	}
	if isPinnedLabImage(ref) {
		return nil
	}
	for _, prefix := range extraImageAllowPrefixes() {
		if prefix == "" {
			continue
		}
		if ref == prefix || strings.HasPrefix(ref, prefix) {
			if imageAllowPrefixNeedsDigest(prefix) && !strings.Contains(ref, "@sha256:") {
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
	}
	_, ok := pinnedExact[lower]
	return ok
}

func imageAllowPrefixNeedsDigest(prefix string) bool {
	host := prefix
	if i := strings.IndexAny(prefix, "/@"); i >= 0 {
		host = prefix[:i]
	}
	return strings.Contains(host, ".") || strings.Contains(host, ":")
}

func extraImageAllowPrefixes() []string {
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
