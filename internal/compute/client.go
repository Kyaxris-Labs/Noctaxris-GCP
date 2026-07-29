// Nested DinD client for opt-in lab container runs (see package docs on invoker.go).
package compute

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/google/uuid"
)

// Client talks to a nested Docker engine (never the host docker.sock by default).
// When DockerHost is empty, Dial returns a disabled Client that no-ops.
type Client struct {
	cli      *client.Client
	disabled bool
}

// Dial connects to dockerHost (e.g. tcp://noctaxris-gcp-engine:2376).
// Empty dockerHost returns a disabled Client with nil error (compute off).
// Non-empty host requires allowlisted tcp:// URL and TLS client PEMs.
func Dial(dockerHost, tlsCertPath string) (*Client, error) {
	if strings.TrimSpace(dockerHost) == "" {
		return &Client{disabled: true}, nil
	}
	if err := ValidateDockerHost(dockerHost, tlsCertPath); err != nil {
		return nil, err
	}
	p := strings.TrimSpace(tlsCertPath)
	opts := []client.Opt{
		client.WithHost(dockerHost),
		client.WithAPIVersionNegotiation(),
		client.WithTLSClientConfig(
			filepath.Join(p, "ca.pem"),
			filepath.Join(p, "cert.pem"),
			filepath.Join(p, "key.pem"),
		),
	}
	cli, err := client.NewClientWithOpts(opts...)
	if err != nil {
		return nil, fmt.Errorf("compute: docker client: %w", err)
	}
	return &Client{cli: cli}, nil
}

// Enabled reports whether a nested engine endpoint is configured.
func (c *Client) Enabled() bool {
	return c != nil && !c.disabled && c.cli != nil
}

// Close releases the underlying Docker HTTP client.
func (c *Client) Close() error {
	if c == nil || c.cli == nil {
		return nil
	}
	return c.cli.Close()
}

// Ping checks that the nested engine is reachable.
func (c *Client) Ping(ctx context.Context) error {
	if !c.Enabled() {
		return fmt.Errorf("compute: engine disabled (NOCTAXRIS_GCP_DOCKER_HOST empty)")
	}
	if _, err := c.cli.Ping(ctx); err != nil {
		return fmt.Errorf("compute: ping engine: %w", err)
	}
	return nil
}

// OneShotResult is the outcome of a nested lab container run.
type OneShotResult struct {
	Image    string
	ExitCode int64
	Stdout   string
}

// RunLabOneShot pulls (if needed) and runs a short-lived allowlisted image.
// Default command echoes a lab token so invoke responses prove nested exec.
func (c *Client) RunLabOneShot(ctx context.Context, imageRef string) (OneShotResult, error) {
	if !c.Enabled() {
		return OneShotResult{}, fmt.Errorf("compute: engine disabled (NOCTAXRIS_GCP_DOCKER_HOST empty)")
	}
	ref := strings.TrimSpace(imageRef)
	if ref == "" {
		ref = DefaultLabImage
	}
	if err := AllowImagePull(ref); err != nil {
		return OneShotResult{}, err
	}
	if err := c.Ping(ctx); err != nil {
		return OneShotResult{}, err
	}
	if err := c.pullImage(ctx, ref); err != nil {
		return OneShotResult{}, err
	}

	name := "noctaxris-gcp-run-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	create, err := c.cli.ContainerCreate(ctx, &container.Config{
		Image: ref,
		Cmd:   []string{"echo", "noctaxris-gcp-nested-ok"},
		Tty:   false,
	}, &container.HostConfig{
		AutoRemove:  false,
		NetworkMode: "none",
	}, nil, nil, name)
	if err != nil {
		return OneShotResult{}, fmt.Errorf("compute: container create: %w", err)
	}
	id := create.ID
	defer func() {
		_ = c.cli.ContainerRemove(context.Background(), id, container.RemoveOptions{Force: true})
	}()

	if err := c.cli.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
		return OneShotResult{}, fmt.Errorf("compute: container start: %w", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	statusCh, errCh := c.cli.ContainerWait(waitCtx, id, container.WaitConditionNotRunning)
	var exitCode int64
	select {
	case err := <-errCh:
		if err != nil {
			return OneShotResult{}, fmt.Errorf("compute: container wait: %w", err)
		}
	case st := <-statusCh:
		if st.Error != nil {
			return OneShotResult{}, fmt.Errorf("compute: container wait: %s", st.Error.Message)
		}
		exitCode = st.StatusCode
	}

	logs, err := c.cli.ContainerLogs(ctx, id, container.LogsOptions{ShowStdout: true, ShowStderr: true})
	if err != nil {
		return OneShotResult{}, fmt.Errorf("compute: container logs: %w", err)
	}
	defer logs.Close()
	raw, _ := io.ReadAll(io.LimitReader(logs, 1<<20))
	return OneShotResult{
		Image:    ref,
		ExitCode: exitCode,
		Stdout:   strings.TrimSpace(stripDockerLogHeader(raw)),
	}, nil
}

// LabDaemonResult is a long-lived nested container started for lab databases/brokers.
type LabDaemonResult struct {
	ContainerID string
	Host        string
	Port        int
}

// LabDaemonNetwork is the shared DinD bridge for nested SQL, Kafka, Redis, and
// related lab daemons. Created on demand inside the engine; no host port publish.
const LabDaemonNetwork = "noctaxris-gcp-lab"

// StartLabDaemon pulls (if needed) and starts a long-lived allowlisted image on the lab bridge network.
// The returned Host is the Docker container name (resolvable from other containers on the same network).
func (c *Client) StartLabDaemon(ctx context.Context, imageRef, containerName string, env []string, port int) (LabDaemonResult, error) {
	if !c.Enabled() {
		return LabDaemonResult{}, fmt.Errorf("compute: engine disabled (NOCTAXRIS_GCP_DOCKER_HOST empty)")
	}
	ref := strings.TrimSpace(imageRef)
	if ref == "" {
		return LabDaemonResult{}, fmt.Errorf("compute: image reference is empty")
	}
	if err := AllowImagePull(ref); err != nil {
		return LabDaemonResult{}, err
	}
	name := strings.TrimSpace(containerName)
	if name == "" {
		return LabDaemonResult{}, fmt.Errorf("compute: container name is empty")
	}
	if err := c.Ping(ctx); err != nil {
		return LabDaemonResult{}, err
	}
	if err := c.ensureLabNetwork(ctx); err != nil {
		return LabDaemonResult{}, err
	}
	if err := c.pullImage(ctx, ref); err != nil {
		return LabDaemonResult{}, err
	}

	create, err := c.cli.ContainerCreate(ctx, &container.Config{
		Image: ref,
		Env:   env,
		ExposedPorts: nat.PortSet{
			nat.Port(fmt.Sprintf("%d/tcp", port)): struct{}{},
		},
	}, &container.HostConfig{
		NetworkMode: container.NetworkMode(LabDaemonNetwork),
		RestartPolicy: container.RestartPolicy{
			Name: "unless-stopped",
		},
	}, nil, nil, name)
	if err != nil {
		return LabDaemonResult{}, fmt.Errorf("compute: daemon create: %w", err)
	}
	id := create.ID
	if err := c.cli.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
		_ = c.cli.ContainerRemove(ctx, id, container.RemoveOptions{Force: true})
		return LabDaemonResult{}, fmt.Errorf("compute: daemon start: %w", err)
	}
	return LabDaemonResult{
		ContainerID: id,
		Host:        name,
		Port:        port,
	}, nil
}

// RemoveLabDaemon force-removes a nested container by ID (no-op when id empty).
func (c *Client) RemoveLabDaemon(ctx context.Context, containerID string) error {
	if !c.Enabled() || strings.TrimSpace(containerID) == "" {
		return nil
	}
	return c.cli.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true})
}

// ExecLabDaemon runs a command inside a nested lab daemon container (SQL users/databases, etc.).
func (c *Client) ExecLabDaemon(ctx context.Context, containerID string, cmd []string) error {
	if !c.Enabled() {
		return fmt.Errorf("compute: engine disabled (NOCTAXRIS_GCP_DOCKER_HOST empty)")
	}
	ref := strings.TrimSpace(containerID)
	if ref == "" || len(cmd) == 0 {
		return fmt.Errorf("compute: lab daemon exec requires container and command")
	}
	execID, err := c.cli.ContainerExecCreate(ctx, ref, container.ExecOptions{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          cmd,
	})
	if err != nil {
		return fmt.Errorf("compute: lab daemon exec create: %w", err)
	}
	hijacked, err := c.cli.ContainerExecAttach(ctx, execID.ID, container.ExecAttachOptions{})
	if err != nil {
		return fmt.Errorf("compute: lab daemon exec attach: %w", err)
	}
	defer hijacked.Close()
	_, _ = io.Copy(io.Discard, hijacked.Reader)
	inspect, err := c.cli.ContainerExecInspect(ctx, execID.ID)
	if err != nil {
		return fmt.Errorf("compute: lab daemon exec inspect: %w", err)
	}
	if inspect.ExitCode != 0 {
		return fmt.Errorf("compute: lab daemon exec exit %d", inspect.ExitCode)
	}
	return nil
}

// EnsureRedpanda starts or reuses a long-lived Redpanda broker on the nested lab network.
func (c *Client) EnsureRedpanda(ctx context.Context, containerName string) (bootstrap, containerID string, err error) {
	if !c.Enabled() {
		return "", "", fmt.Errorf("compute: engine disabled (NOCTAXRIS_GCP_DOCKER_HOST empty)")
	}
	name := strings.TrimSpace(containerName)
	if name == "" {
		return "", "", fmt.Errorf("compute: redpanda container name is empty")
	}
	ref := LabRedpandaImage
	if err := AllowImagePull(ref); err != nil {
		return "", "", err
	}
	if err := c.Ping(ctx); err != nil {
		return "", "", err
	}
	if err := c.ensureLabNetwork(ctx); err != nil {
		return "", "", err
	}

	inspect, err := c.cli.ContainerInspect(ctx, name)
	if err == nil {
		id := inspect.ID
		if inspect.State != nil && !inspect.State.Running {
			if err := c.cli.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
				return "", "", fmt.Errorf("compute: redpanda start existing: %w", err)
			}
		}
		return name + ":9092", id, nil
	}

	if err := c.pullImage(ctx, ref); err != nil {
		return "", "", err
	}
	create, err := c.cli.ContainerCreate(ctx, &container.Config{
		Image: ref,
		Cmd:   RedpandaStartCmd(name),
		Tty:   false,
		Labels: map[string]string{
			"noctaxris-gcp.kind": "managedkafka",
		},
		ExposedPorts: nat.PortSet{
			"9092/tcp": struct{}{},
		},
	}, &container.HostConfig{
		NetworkMode: container.NetworkMode(LabDaemonNetwork),
		RestartPolicy: container.RestartPolicy{
			Name: "unless-stopped",
		},
	}, nil, nil, name)
	if err != nil {
		return "", "", fmt.Errorf("compute: redpanda create: %w", err)
	}
	id := create.ID
	if err := c.cli.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
		_ = c.cli.ContainerRemove(ctx, id, container.RemoveOptions{Force: true})
		return "", "", fmt.Errorf("compute: redpanda start: %w", err)
	}
	return name + ":9092", id, nil
}

// RemoveRedpanda stops and removes a nested broker by container name.
func (c *Client) RemoveRedpanda(ctx context.Context, containerName string) error {
	if !c.Enabled() {
		return nil
	}
	name := strings.TrimSpace(containerName)
	if name == "" {
		return nil
	}
	timeout := 10
	_ = c.cli.ContainerStop(ctx, name, container.StopOptions{Timeout: &timeout})
	return c.cli.ContainerRemove(ctx, name, container.RemoveOptions{Force: true})
}

// CreateRedpandaTopic best-effort creates a topic inside a nested Redpanda via rpk.
// Soft callers should ignore errors when the engine is off or the container is missing.
func (c *Client) CreateRedpandaTopic(ctx context.Context, containerRef, topic string, partitions, replicationFactor int) error {
	if !c.Enabled() {
		return fmt.Errorf("compute: engine disabled (NOCTAXRIS_GCP_DOCKER_HOST empty)")
	}
	ref := strings.TrimSpace(containerRef)
	topic = strings.TrimSpace(topic)
	if ref == "" || topic == "" {
		return fmt.Errorf("compute: redpanda topic create requires container and topic")
	}
	if partitions <= 0 {
		partitions = 1
	}
	if replicationFactor <= 0 {
		replicationFactor = 1
	}
	cmd := []string{
		"rpk", "topic", "create", topic,
		"-p", fmt.Sprintf("%d", partitions),
		"-r", fmt.Sprintf("%d", replicationFactor),
	}
	execID, err := c.cli.ContainerExecCreate(ctx, ref, container.ExecOptions{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          cmd,
	})
	if err != nil {
		return fmt.Errorf("compute: redpanda topic exec create: %w", err)
	}
	hijacked, err := c.cli.ContainerExecAttach(ctx, execID.ID, container.ExecAttachOptions{})
	if err != nil {
		return fmt.Errorf("compute: redpanda topic exec attach: %w", err)
	}
	defer hijacked.Close()
	_, _ = io.Copy(io.Discard, hijacked.Reader)
	inspect, err := c.cli.ContainerExecInspect(ctx, execID.ID)
	if err != nil {
		return fmt.Errorf("compute: redpanda topic exec inspect: %w", err)
	}
	if inspect.ExitCode != 0 {
		return fmt.Errorf("compute: rpk topic create exit %d", inspect.ExitCode)
	}
	return nil
}

// RedpandaContainerNameForCluster returns the stable nested container name for a cluster id.
func RedpandaContainerNameForCluster(clusterID string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			return r
		default:
			return '-'
		}
	}, strings.TrimSpace(clusterID))
	if safe == "" {
		safe = "cluster"
	}
	return "noctaxris-gcp-kafka-" + safe
}

func (c *Client) ensureLabNetwork(ctx context.Context) error {
	_, err := c.cli.NetworkInspect(ctx, LabDaemonNetwork, network.InspectOptions{})
	if err == nil {
		return nil
	}
	_, err = c.cli.NetworkCreate(ctx, LabDaemonNetwork, network.CreateOptions{
		Driver: "bridge",
		Labels: map[string]string{"noctaxris-gcp": "lab"},
	})
	if err != nil {
		return fmt.Errorf("compute: create lab network: %w", err)
	}
	return nil
}

func (c *Client) pullImage(ctx context.Context, ref string) error {
	rc, err := c.cli.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("compute: pull %s: %w", ref, err)
	}
	defer rc.Close()
	_, _ = io.Copy(io.Discard, rc)
	return nil
}

// Docker multiplexes stdout/stderr with an 8-byte header per frame; strip when present.
func stripDockerLogHeader(b []byte) string {
	if len(b) >= 8 && (b[0] == 1 || b[0] == 2) {
		return string(b[8:])
	}
	return string(b)
}
