package compute

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/errdefs"
	"github.com/docker/go-connections/nat"
)

// MemorystoreRedisImage is the pinned nested Memorystore for Redis lab engine.
const MemorystoreRedisImage = "redis:7-alpine"

// MemorystoreRedisNetwork is the shared lab DinD bridge (same as SQL/Kafka).
// Alias of LabDaemonNetwork so Redis can resolve nested SQL/Kafka by DNS.
const MemorystoreRedisNetwork = LabDaemonNetwork

const memorystoreRedisPort = 6379

const labelMemorystoreManaged = "noctaxris.gcp.managed"
const labelMemorystoreInstance = "noctaxris.gcp.memorystore"

// MemorystoreRedisContainerName is the nested-network DNS name for an instance id.
func MemorystoreRedisContainerName(instanceID string) string {
	safe := strings.ToLower(strings.TrimSpace(instanceID))
	safe = strings.ReplaceAll(safe, "_", "-")
	var b strings.Builder
	b.WriteString("noctaxris-gcp-redis-")
	for _, r := range safe {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "noctaxris-gcp-redis" {
		return "noctaxris-gcp-redis-lab"
	}
	if len(out) > 63 {
		out = out[:63]
	}
	return out
}

// MemorystoreRedisAuthEnv returns REDIS_PASSWORD env for nested redis when AUTH is set.
func MemorystoreRedisAuthEnv(authPassword string) []string {
	pw := strings.TrimSpace(authPassword)
	if pw == "" {
		return nil
	}
	return []string{"REDIS_PASSWORD=" + pw}
}

// MemorystoreRedisAuthCmd returns redis-server --requirepass args when AUTH is set.
func MemorystoreRedisAuthCmd(authPassword string) []string {
	pw := strings.TrimSpace(authPassword)
	if pw == "" {
		return nil
	}
	return []string{"redis-server", "--requirepass", pw}
}

// MemorystoreRedisResult describes a nested Redis container on the lab bridge.
type MemorystoreRedisResult struct {
	Host        string
	ContainerID string
	Port        int
}

// EnsureMemorystoreRedisFromEnv dials the nested engine when NOCTAXRIS_GCP_DOCKER_HOST is set
// and starts (or reuses) a detached redis:7-alpine container. Empty docker host is a no-op.
// Non-empty authPassword sets REDIS_PASSWORD and redis-server --requirepass.
func EnsureMemorystoreRedisFromEnv(ctx context.Context, instanceID, authPassword string) (MemorystoreRedisResult, error) {
	host := strings.TrimSpace(osGetenv(EnvDockerHost))
	if host == "" {
		return MemorystoreRedisResult{}, nil
	}
	cert := strings.TrimSpace(osGetenv(EnvDockerCertPath))
	cli, err := Dial(host, cert)
	if err != nil {
		return MemorystoreRedisResult{}, err
	}
	defer cli.Close()
	return cli.EnsureMemorystoreRedis(ctx, instanceID, authPassword)
}

// RemoveMemorystoreRedisFromEnv stops and removes a nested Redis container when configured.
func RemoveMemorystoreRedisFromEnv(ctx context.Context, instanceID, containerID string) error {
	host := strings.TrimSpace(osGetenv(EnvDockerHost))
	if host == "" {
		return nil
	}
	cert := strings.TrimSpace(osGetenv(EnvDockerCertPath))
	cli, err := Dial(host, cert)
	if err != nil {
		return err
	}
	defer cli.Close()
	return cli.RemoveMemorystoreRedis(ctx, instanceID, containerID)
}

// EnsureMemorystoreRedis starts or reuses a nested Redis container (no host port publish).
// When authPassword is non-empty, the container runs with REDIS_PASSWORD and --requirepass.
func (c *Client) EnsureMemorystoreRedis(ctx context.Context, instanceID, authPassword string) (MemorystoreRedisResult, error) {
	if !c.Enabled() {
		return MemorystoreRedisResult{}, fmt.Errorf("compute: engine disabled")
	}
	id := strings.TrimSpace(instanceID)
	if id == "" {
		return MemorystoreRedisResult{}, fmt.Errorf("compute: memorystore instance id is required")
	}
	if err := AllowImagePull(MemorystoreRedisImage); err != nil {
		return MemorystoreRedisResult{}, err
	}
	name := MemorystoreRedisContainerName(id)
	if err := c.ensureLabNetwork(ctx); err != nil {
		return MemorystoreRedisResult{}, err
	}
	if err := c.pullImage(ctx, MemorystoreRedisImage); err != nil {
		return MemorystoreRedisResult{}, err
	}

	insp, err := c.cli.ContainerInspect(ctx, name)
	if err == nil {
		cid := insp.ID
		if !insp.State.Running {
			if err := c.cli.ContainerStart(ctx, cid, container.StartOptions{}); err != nil {
				return MemorystoreRedisResult{}, fmt.Errorf("compute: memorystore redis start: %w", err)
			}
		}
		return MemorystoreRedisResult{
			Host:        name,
			ContainerID: cid,
			Port:        memorystoreRedisPort,
		}, nil
	}
	if !errdefs.IsNotFound(err) {
		return MemorystoreRedisResult{}, fmt.Errorf("compute: memorystore redis inspect: %w", err)
	}

	labels := map[string]string{
		labelMemorystoreManaged:  "true",
		labelMemorystoreInstance: id,
	}
	cfg := &container.Config{
		Image:  MemorystoreRedisImage,
		Labels: labels,
		Env:    MemorystoreRedisAuthEnv(authPassword),
		Cmd:    MemorystoreRedisAuthCmd(authPassword),
		ExposedPorts: nat.PortSet{
			nat.Port(fmt.Sprintf("%d/tcp", memorystoreRedisPort)): struct{}{},
		},
	}
	create, err := c.cli.ContainerCreate(ctx, cfg, &container.HostConfig{
		AutoRemove:      false,
		NetworkMode:     container.NetworkMode(MemorystoreRedisNetwork),
		PublishAllPorts: false,
		RestartPolicy: container.RestartPolicy{
			Name: "unless-stopped",
		},
	}, nil, nil, name)
	if err != nil {
		return MemorystoreRedisResult{}, fmt.Errorf("compute: memorystore redis create: %w", err)
	}
	cid := create.ID
	if err := c.cli.ContainerStart(ctx, cid, container.StartOptions{}); err != nil {
		_ = c.cli.ContainerRemove(context.Background(), cid, container.RemoveOptions{Force: true})
		return MemorystoreRedisResult{}, fmt.Errorf("compute: memorystore redis start: %w", err)
	}
	return MemorystoreRedisResult{
		Host:        name,
		ContainerID: cid,
		Port:        memorystoreRedisPort,
	}, nil
}

// RemoveMemorystoreRedis removes a nested Redis container by id or by instance-derived name.
func (c *Client) RemoveMemorystoreRedis(ctx context.Context, instanceID, containerID string) error {
	if !c.Enabled() {
		return nil
	}
	target := strings.TrimSpace(containerID)
	if target == "" {
		target = MemorystoreRedisContainerName(instanceID)
	}
	err := c.cli.ContainerRemove(ctx, target, container.RemoveOptions{Force: true})
	if err != nil && !errdefs.IsNotFound(err) {
		return fmt.Errorf("compute: memorystore redis remove: %w", err)
	}
	return nil
}

func osGetenv(key string) string {
	return os.Getenv(key)
}
