package memorystore

import (
	"context"
	"os"
	"strings"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/compute"
)

func (s *Service) tryNestedRedisOnCreate(ctx context.Context, name, instanceID string) {
	host := strings.TrimSpace(os.Getenv("NOCTAXRIS_GCP_DOCKER_HOST"))
	if host == "" {
		return
	}
	cli, err := compute.Dial(host, os.Getenv("NOCTAXRIS_GCP_DOCKER_CERT_PATH"))
	if err != nil || !cli.Enabled() {
		return
	}
	defer func() { _ = cli.Close() }()
	res, err := cli.EnsureMemorystoreRedis(ctx, instanceID)
	if err != nil {
		return
	}
	_ = s.Store.SetMemorystoreRedisRuntime(name, res.Host, res.ContainerID, res.Port)
}

func (s *Service) tryNestedRedisOnDelete(ctx context.Context, instanceID, containerID string) {
	host := strings.TrimSpace(os.Getenv("NOCTAXRIS_GCP_DOCKER_HOST"))
	if host == "" {
		return
	}
	cli, err := compute.Dial(host, os.Getenv("NOCTAXRIS_GCP_DOCKER_CERT_PATH"))
	if err != nil || !cli.Enabled() {
		return
	}
	defer func() { _ = cli.Close() }()
	_ = cli.RemoveMemorystoreRedis(ctx, instanceID, containerID)
}
