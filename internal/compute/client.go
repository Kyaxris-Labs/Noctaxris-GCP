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
	"github.com/docker/docker/client"
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
