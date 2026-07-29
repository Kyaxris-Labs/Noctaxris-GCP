package compute_test

import (
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/compute"
)

func TestRedpandaContainerNameForCluster(t *testing.T) {
	t.Parallel()
	name := compute.RedpandaContainerNameForCluster("lab/kafka_1")
	if name != "noctaxris-gcp-kafka-lab-kafka-1" {
		t.Fatalf("name=%q", name)
	}
}
