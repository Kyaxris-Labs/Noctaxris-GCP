package compute_test

import (
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/compute"
)

func TestRedpandaContainerNameForClusterScoped(t *testing.T) {
	t.Parallel()
	a := compute.RedpandaContainerNameForCluster("proj-a", "us-central1", "shared")
	b := compute.RedpandaContainerNameForCluster("proj-b", "us-central1", "shared")
	c := compute.RedpandaContainerNameForCluster("proj-a", "us-central1", "foo_bar")
	d := compute.RedpandaContainerNameForCluster("proj-a", "us-central1", "foo/bar")
	if a == "" || !hasPrefix(a, "noctaxris-gcp-kafka-") {
		t.Fatalf("name=%q", a)
	}
	if a == b {
		t.Fatal("same clusterId in different projects must not share container name")
	}
	if c == d {
		t.Fatal("punctuation-collapsed cluster ids must not share container name")
	}
}

func hasPrefix(s, p string) bool {
	return len(s) >= len(p) && s[:len(p)] == p
}
