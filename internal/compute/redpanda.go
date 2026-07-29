package compute

import "strings"

// LabRedpandaImage is the pinned Redpanda image for Managed Kafka nested brokers.
const LabRedpandaImage = "docker.redpanda.com/redpandadata/redpanda:v24.2.4"

// RedpandaStartCmd returns Redpanda start args for a nested Kafka broker.
// advertiseHost is the nested-network hostname clients use (container name).
func RedpandaStartCmd(advertiseHost string) []string {
	host := strings.TrimSpace(advertiseHost)
	if host == "" {
		host = "noctaxris-gcp-kafka"
	}
	return []string{
		"redpanda", "start",
		"--overprovisioned",
		"--smp", "1",
		"--memory", "512M",
		"--reserve-memory", "0M",
		"--node-id", "0",
		"--check=false",
		"--kafka-addr", "internal://0.0.0.0:9092",
		"--advertise-kafka-addr", "internal://" + host + ":9092",
	}
}
