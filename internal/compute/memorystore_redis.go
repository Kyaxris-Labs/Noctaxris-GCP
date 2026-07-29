package compute

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/errdefs"
	"github.com/docker/go-connections/nat"
)

// MemorystoreRedisImage is the pinned nested Memorystore for Redis lab engine.
const MemorystoreRedisImage = "redis:7-alpine"

// MemorystoreRedisNetwork is the internal DinD network for nested Redis (no host publish).
const MemorystoreRedisNetwork = "noctaxris-gcp-data"

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

// MemorystoreRedisResult describes a nested Redis container on the lab data network.
type MemorystoreRedisResult struct {
	Host        string
	ContainerID string
	Port        int
}

// EnsureMemorystoreRedisFromEnv dials the nested engine when NOCTAXRIS_GCP_DOCKER_HOST is set
// and starts (or reuses) a detached redis:7-alpine container. Empty docker host is a no-op.
func EnsureMemorystoreRedisFromEnv(ctx context.Context, instanceID string) (MemorystoreRedisResult, error) {
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
	return cli.EnsureMemorystoreRedis(ctx, instanceID)
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
func (c *Client) EnsureMemorystoreRedis(ctx context.Context, instanceID string) (MemorystoreRedisResult, error) {
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
	if _, err := c.ensureMemorystoreNetwork(ctx); err != nil {
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
	netCfg := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			MemorystoreRedisNetwork: {},
		},
	}
	create, err := c.cli.ContainerCreate(ctx, &container.Config{
		Image: MemorystoreRedisImage,
		Labels: labels,
		ExposedPorts: nat.PortSet{
			nat.Port(fmt.Sprintf("%d/tcp", memorystoreRedisPort)): struct{}{},
		},
	}, &container.HostConfig{
		AutoRemove:      false,
		NetworkMode:     container.NetworkMode(MemorystoreRedisNetwork),
		PublishAllPorts: false,
	}, netCfg, nil, name)
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

func (c *Client) ensureMemorystoreNetwork(ctx context.Context) (string, error) {
	if c == nil || c.cli == nil {
		return "", fmt.Errorf("compute: client unavailable")
	}
	name := MemorystoreRedisNetwork
	networks, err := c.cli.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("compute: list networks: %w", err)
	}
	for _, n := range networks {
		if n.Name != name {
			continue
		}
		insp, err := c.cli.NetworkInspect(ctx, n.ID, network.InspectOptions{})
		if err != nil {
			return "", fmt.Errorf("compute: inspect network %s: %w", name, err)
		}
		if !insp.Internal {
			return "", fmt.Errorf("compute: network %s exists but is not Internal (refuse reuse)", name)
		}
		return n.ID, nil
	}
	resp, err := c.cli.NetworkCreate(ctx, name, network.CreateOptions{
		Driver:   "bridge",
		Internal: true,
		Labels: map[string]string{
			labelMemorystoreManaged: "true",
		},
	})
	if err != nil {
		return "", fmt.Errorf("compute: create network %s: %w", name, err)
	}
	return resp.ID, nil
}

func osGetenv(key string) string {
	return os.Getenv(key)
}
