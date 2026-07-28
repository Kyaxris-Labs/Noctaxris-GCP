package compute_test

import (
	"strings"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/compute"
)

func TestAllowImagePullPinned(t *testing.T) {
	t.Parallel()
	ok := []string{
		compute.DefaultLabImage,
		"alpine:3.20",
		"public.ecr.aws/docker/library/alpine:3.20",
		"gcr.io/google-containers/pause:3.9",
	}
	for _, ref := range ok {
		if err := compute.AllowImagePull(ref); err != nil {
			t.Fatalf("%s: %v", ref, err)
		}
	}
}

func TestAllowImagePullRejectsUnknown(t *testing.T) {
	t.Parallel()
	err := compute.AllowImagePull("evil.registry.example/malware:latest")
	if err == nil {
		t.Fatal("expected reject")
	}
	if !strings.Contains(err.Error(), "not allowlisted") {
		t.Fatalf("error=%v", err)
	}
	err = compute.AllowImagePull("us-docker.pkg.dev/demo/hello:latest")
	if err == nil {
		t.Fatal("expected reject for unpinned Artifact Registry image")
	}
}

func TestAllowImagePullEmpty(t *testing.T) {
	t.Parallel()
	if err := compute.AllowImagePull(""); err == nil {
		t.Fatal("expected reject for empty ref")
	}
}

func TestAllowImagePullExtraAllowlistRequiresDigest(t *testing.T) {
	t.Setenv(compute.EnvImagePullAllowlist, "ghcr.io/kyaxris-labs/")
	err := compute.AllowImagePull("ghcr.io/kyaxris-labs/tool:latest")
	if err == nil || !strings.Contains(err.Error(), "digest") {
		t.Fatalf("expected digest requirement, got %v", err)
	}
	if err := compute.AllowImagePull("ghcr.io/kyaxris-labs/tool@sha256:"+strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
}

func TestAllowImagePullRejectsAmbiguousPrefix(t *testing.T) {
	t.Setenv(compute.EnvImagePullAllowlist, "alpine")
	if err := compute.AllowImagePull("alpine-evil:latest"); err == nil {
		t.Fatal("bare prefix without trailing slash must not match")
	}
	t.Setenv(compute.EnvImagePullAllowlist, "ghcr.io/kyaxris-labs/tool@sha256:"+strings.Repeat("b", 64))
	exact := "ghcr.io/kyaxris-labs/tool@sha256:" + strings.Repeat("b", 64)
	if err := compute.AllowImagePull(exact); err != nil {
		t.Fatal(err)
	}
}
