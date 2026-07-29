package memorystore

import (
	"context"
	"os"
	"strings"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/compute"
)

func (s *Service) tryNestedRedisOnCreate(ctx context.Context, name, instanceID, authPassword string) error {
	cli, owned, err := s.nestEngine()
	if err != nil {
		if compute.NestedEngineFailClosed() {
			return err
		}
		return nil
	}
	if cli == nil || !cli.Enabled() {
		return nil
	}
	if owned {
		defer func() { _ = cli.Close() }()
	}
	res, err := cli.EnsureMemorystoreRedis(ctx, instanceID, authPassword)
	if err != nil {
		if compute.NestedEngineFailClosed() {
			return err
		}
		return nil
	}
	_ = s.Store.SetMemorystoreRedisRuntime(name, res.Host, res.ContainerID, res.Port)
	return nil
}

func (s *Service) tryNestedRedisOnDelete(ctx context.Context, instanceID, containerID string) {
	cli, owned, err := s.nestEngine()
	if err != nil || cli == nil || !cli.Enabled() {
		return
	}
	if owned {
		defer func() { _ = cli.Close() }()
	}
	_ = cli.RemoveMemorystoreRedis(ctx, instanceID, containerID)
}

// nestEngine returns the nested Redis client. owned=true means the caller must Close.
func (s *Service) nestEngine() (cli NestEngine, owned bool, err error) {
	if s.Engine != nil {
		return s.Engine, false, nil
	}
	host := strings.TrimSpace(os.Getenv(compute.EnvDockerHost))
	if host == "" {
		return nil, false, nil
	}
	c, err := compute.Dial(host, os.Getenv(compute.EnvDockerCertPath))
	if err != nil {
		return nil, false, err
	}
	return c, true, nil
}
