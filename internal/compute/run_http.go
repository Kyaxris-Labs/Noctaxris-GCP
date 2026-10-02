package compute

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

// EnvRunPublishPort pins the engine host port used for Cloud Run nested HTTP
// (decimal 1-65535). Unset or empty uses an ephemeral engine port. A fixed port
// supports one nested Run service at a time per engine.
const EnvRunPublishPort = "NOCTAXRIS_GCP_RUN_PUBLISH_PORT"

// DefaultRunContainerPort is the Cloud Run container port when the template omits one.
const DefaultRunContainerPort = 8080

// RunHTTPOptions describes a long-lived nested Cloud Run HTTP container.
type RunHTTPOptions struct {
	Image         string
	Name          string
	Env           []string
	ContainerPort int
	// Entrypoint and Cmd map to Cloud Run container command and args.
	Entrypoint []string
	Cmd        []string
}

// RunHTTPResult is a long-lived nested container reachable on the engine host.
type RunHTTPResult struct {
	ContainerID   string
	HostPort      int
	ContainerPort int
	// EngineHost is the hostname from NOCTAXRIS_GCP_DOCKER_HOST (dial target for HostPort).
	EngineHost string
}

// RunHTTPContainerName returns a stable nested container name for a Cloud Run service.
func RunHTTPContainerName(project, location, serviceID string) string {
	key := strings.TrimSpace(project) + "\x00" + strings.TrimSpace(location) + "\x00" + strings.TrimSpace(serviceID)
	if strings.TrimSpace(project) == "" || strings.TrimSpace(location) == "" || strings.TrimSpace(serviceID) == "" {
		key = "invalid\x00" + key
	}
	sum := sha256.Sum256([]byte(key))
	return "noctaxris-gcp-run-http-" + hex.EncodeToString(sum[:12])
}

// EngineHostFromDockerHost returns the hostname in a tcp:// engine URL.
func EngineHostFromDockerHost(dockerHost string) (string, error) {
	host := strings.TrimSpace(dockerHost)
	if host == "" {
		return "", fmt.Errorf("compute: docker host is empty")
	}
	u, err := url.Parse(host)
	if err != nil {
		return "", fmt.Errorf("compute: parse docker host: %w", err)
	}
	if !strings.EqualFold(u.Scheme, "tcp") {
		return "", fmt.Errorf("compute: docker host scheme %q is not tcp", u.Scheme)
	}
	name := u.Hostname()
	if name == "" {
		return "", fmt.Errorf("compute: docker host has no hostname")
	}
	return name, nil
}

// RunPublishPortFromEnv parses EnvRunPublishPort. Zero means ephemeral.
func RunPublishPortFromEnv() (int, error) {
	raw := strings.TrimSpace(os.Getenv(EnvRunPublishPort))
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 65535 {
		return 0, fmt.Errorf("compute: %s must be an integer from 1 to 65535", EnvRunPublishPort)
	}
	return n, nil
}

// StartRunHTTP starts a long-lived allowlisted image on the lab bridge with the
// container port published on the engine (ephemeral unless EnvRunPublishPort is set).
func (c *Client) StartRunHTTP(ctx context.Context, image, name string, env []string, containerPort int) (RunHTTPResult, error) {
	return c.StartRunHTTPWithOptions(ctx, RunHTTPOptions{
		Image:         image,
		Name:          name,
		Env:           env,
		ContainerPort: containerPort,
	})
}

// StartRunHTTPWithOptions is StartRunHTTP with entrypoint and command overrides.
// The image must pass AllowImagePull. A local engine image is used without a
// registry pull when present (for example after docker load inside the engine).
func (c *Client) StartRunHTTPWithOptions(ctx context.Context, opts RunHTTPOptions) (RunHTTPResult, error) {
	if !c.Enabled() {
		return RunHTTPResult{}, fmt.Errorf("compute: engine disabled (NOCTAXRIS_GCP_DOCKER_HOST empty)")
	}
	ref := strings.TrimSpace(opts.Image)
	if ref == "" {
		return RunHTTPResult{}, fmt.Errorf("compute: image reference is empty")
	}
	if err := AllowImagePull(ref); err != nil {
		return RunHTTPResult{}, err
	}
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		return RunHTTPResult{}, fmt.Errorf("compute: container name is empty")
	}
	containerPort := opts.ContainerPort
	if containerPort == 0 {
		containerPort = DefaultRunContainerPort
	}
	if containerPort < 1 || containerPort > 65535 {
		return RunHTTPResult{}, fmt.Errorf("compute: container port %d out of range", containerPort)
	}
	publish, err := RunPublishPortFromEnv()
	if err != nil {
		return RunHTTPResult{}, err
	}
	engineHost, err := EngineHostFromDockerHost(os.Getenv(EnvDockerHost))
	if err != nil {
		return RunHTTPResult{}, err
	}
	if err := c.Ping(ctx); err != nil {
		return RunHTTPResult{}, err
	}
	if err := c.ensureLabNetwork(ctx); err != nil {
		return RunHTTPResult{}, err
	}
	if err := c.ensureImage(ctx, ref); err != nil {
		return RunHTTPResult{}, err
	}
	// Replace any prior container for this service name (patch or reseed).
	_, _ = c.cli.ContainerRemove(ctx, name, client.ContainerRemoveOptions{Force: true})

	port := network.MustParsePort(fmt.Sprintf("%d/tcp", containerPort))
	create, err := c.cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image:        ref,
			Env:          opts.Env,
			Entrypoint:   opts.Entrypoint,
			Cmd:          opts.Cmd,
			ExposedPorts: network.PortSet{port: struct{}{}},
		},
		HostConfig: runHostConfig(port, publish),
		Name:       name,
	})
	if err != nil {
		return RunHTTPResult{}, fmt.Errorf("compute: run http create: %w", err)
	}
	id := create.ID
	if _, err := c.cli.ContainerStart(ctx, id, client.ContainerStartOptions{}); err != nil {
		_, _ = c.cli.ContainerRemove(ctx, id, client.ContainerRemoveOptions{Force: true})
		return RunHTTPResult{}, fmt.Errorf("compute: run http start: %w", err)
	}
	inspect, err := c.cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		_, _ = c.cli.ContainerRemove(ctx, id, client.ContainerRemoveOptions{Force: true})
		return RunHTTPResult{}, fmt.Errorf("compute: run http inspect: %w", err)
	}
	var ports network.PortMap
	if inspect.Container.NetworkSettings != nil {
		ports = inspect.Container.NetworkSettings.Ports
	}
	hostPort, err := hostPortFromPortMap(ports, containerPort)
	if err != nil {
		_, _ = c.cli.ContainerRemove(ctx, id, client.ContainerRemoveOptions{Force: true})
		return RunHTTPResult{}, err
	}
	return RunHTTPResult{
		ContainerID:   id,
		HostPort:      hostPort,
		ContainerPort: containerPort,
		EngineHost:    engineHost,
	}, nil
}

// RemoveRunHTTP force-removes a nested Cloud Run HTTP container (no-op when id empty).
func (c *Client) RemoveRunHTTP(ctx context.Context, containerID string) error {
	return c.RemoveLabDaemon(ctx, containerID)
}

// runHostConfig is the nested Run container host config. ExtraHosts follows
// NOCTAXRIS_GCP_INJECT_HOST_GATEWAY, same as Cloud Build steps, so the app can
// dial the lab API at host.docker.internal:4588.
func runHostConfig(port network.Port, publish int) *container.HostConfig {
	cfg := &container.HostConfig{
		NetworkMode:  container.NetworkMode(LabDaemonNetwork),
		Sysctls:      LabNetSysctls(),
		PortBindings: runPortBindings(port, publish),
		RestartPolicy: container.RestartPolicy{
			Name: container.RestartPolicyUnlessStopped,
		},
	}
	if hosts := HostGatewayExtraHosts(); len(hosts) > 0 {
		cfg.ExtraHosts = hosts
	}
	return cfg
}

func runPortBindings(port network.Port, publish int) network.PortMap {
	hostPort := ""
	if publish > 0 {
		hostPort = strconv.Itoa(publish)
	}
	return network.PortMap{
		port: []network.PortBinding{{HostIP: netip.Addr{}, HostPort: hostPort}},
	}
}

func hostPortFromPortMap(ports network.PortMap, containerPort int) (int, error) {
	key := network.MustParsePort(fmt.Sprintf("%d/tcp", containerPort))
	for _, b := range ports[key] {
		n, err := strconv.Atoi(strings.TrimSpace(b.HostPort))
		if err == nil && n > 0 {
			return n, nil
		}
	}
	return 0, fmt.Errorf("compute: run http: engine did not publish container port %d", containerPort)
}

func (c *Client) ensureImage(ctx context.Context, ref string) error {
	if _, err := c.cli.ImageInspect(ctx, ref); err == nil {
		return nil
	}
	return c.pullImage(ctx, ref)
}
