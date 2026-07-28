// Package compute provides optional nested-container hooks for lab invoke paths.
//
// Empty NOCTAXRIS_GCP_DOCKER_HOST disables the engine (unit tests need no Docker).
// The API never mounts host docker.sock; Compose overlay compose.engine.yaml
// wires tcp://noctaxris-gcp-engine:2376 with TLS client PEMs.
package compute

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// EnvDockerHost is the opt-in nested engine endpoint (TLS DinD). Empty = mock only.
const EnvDockerHost = "NOCTAXRIS_GCP_DOCKER_HOST"

// EnvDockerCertPath is the TLS material directory (ca.pem, cert.pem, key.pem).
const EnvDockerCertPath = "NOCTAXRIS_GCP_DOCKER_CERT_PATH"

// InvokeRequest is the lab invoke payload passed to an Invoker.
type InvokeRequest struct {
	ServiceName string
	Method      string
	Path        string
	Query       string
	Headers     map[string]string
	Body        []byte
	// StatusCode is the theatre HTTP status (default 200).
	StatusCode int
	// Delay is optional sleep theatre before responding.
	Delay time.Duration
	// ResponseBody is the bytes to return (may be empty for default JSON).
	ResponseBody []byte
	// Image is the container image from the service template (hook for nested invoke).
	Image string
	// Env is container env from the service template.
	Env map[string]string
}

// InvokeResult is the theatre HTTP response from an Invoker.
type InvokeResult struct {
	StatusCode int
	Body       []byte
	Headers    map[string]string
}

// Invoker runs Cloud Run (and future) lab invokes. Nested container paths implement
// this interface when an engine host is configured; tests use MockInvoker.
type Invoker interface {
	Invoke(ctx context.Context, req InvokeRequest) (InvokeResult, error)
}

// MockInvoker is the default in-process invoke theatre (no Docker, no DinD).
type MockInvoker struct{}

// Invoke applies status/delay theatre and returns the configured body.
func (MockInvoker) Invoke(ctx context.Context, req InvokeRequest) (InvokeResult, error) {
	if req.Delay > 0 {
		select {
		case <-ctx.Done():
			return InvokeResult{}, ctx.Err()
		case <-time.After(req.Delay):
		}
	}
	code := req.StatusCode
	if code == 0 {
		code = 200
	}
	body := req.ResponseBody
	if len(body) == 0 {
		body = []byte(`{"ok":true}`)
	}
	return InvokeResult{
		StatusCode: code,
		Body:       body,
		Headers:    map[string]string{"Content-Type": "application/json; charset=utf-8"},
	}, nil
}

// DockerInvoker dials the opt-in nested engine for a short allowlisted one-shot.
// Soft-fails to Fallback (mock) with engine status detail when dial/run fails.
// Never mounts host docker.sock.
type DockerInvoker struct {
	Host       string
	TLSCertDir string
	Fallback   Invoker
}

// Invoke attempts nested RunLabOneShot; on failure soft-fails to mock with detail.
func (d DockerInvoker) Invoke(ctx context.Context, req InvokeRequest) (InvokeResult, error) {
	if strings.TrimSpace(d.Host) == "" {
		return InvokeResult{}, fmt.Errorf("compute: DockerInvoker requires non-empty host (set %s)", EnvDockerHost)
	}
	if isHostDockerSock(d.Host) {
		return InvokeResult{}, fmt.Errorf("compute: host docker.sock is not allowed")
	}

	fb := d.Fallback
	if fb == nil {
		fb = MockInvoker{}
	}

	cli, err := Dial(d.Host, d.TLSCertDir)
	if err != nil {
		return softFailMock(ctx, fb, req, "engine dial failed")
	}
	defer cli.Close()
	if !cli.Enabled() {
		return softFailMock(ctx, fb, req, "engine disabled")
	}

	image := DefaultLabImage
	if ref := strings.TrimSpace(req.Image); ref != "" {
		if err := AllowImagePull(ref); err == nil {
			image = ref
		}
	}
	out, err := cli.RunLabOneShot(ctx, image)
	if err != nil {
		return softFailMock(ctx, fb, req, "engine run failed")
	}

	body, _ := json.Marshal(map[string]any{
		"ok":      true,
		"service": req.ServiceName,
		"env":     req.Env,
		"engine": map[string]any{
			"mode":     "nested",
			"image":    out.Image,
			"exitCode": out.ExitCode,
			"stdout":   out.Stdout,
		},
	})
	code := req.StatusCode
	if code == 0 {
		code = 200
	}
	return InvokeResult{
		StatusCode: code,
		Body:       body,
		Headers:    map[string]string{"Content-Type": "application/json; charset=utf-8"},
	}, nil
}

func softFailMock(ctx context.Context, fb Invoker, req InvokeRequest, detail string) (InvokeResult, error) {
	res, err := fb.Invoke(ctx, req)
	if err != nil {
		return InvokeResult{}, err
	}
	res.Body = mergeEngineDetail(res.Body, "mock", detail)
	if res.Headers == nil {
		res.Headers = map[string]string{}
	}
	if res.Headers["Content-Type"] == "" {
		res.Headers["Content-Type"] = "application/json; charset=utf-8"
	}
	return res, nil
}

func mergeEngineDetail(body []byte, mode, detail string) []byte {
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil || m == nil {
		wrapped, _ := json.Marshal(map[string]any{
			"ok":   true,
			"body": string(body),
			"engine": map[string]any{
				"mode":   mode,
				"detail": detail,
			},
		})
		return wrapped
	}
	m["engine"] = map[string]any{
		"mode":   mode,
		"detail": detail,
	}
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return out
}

func isHostDockerSock(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	return strings.Contains(h, "docker.sock") || h == "unix:///var/run/docker.sock"
}

// NewInvoker returns MockInvoker when host is empty; otherwise DockerInvoker.
func NewInvoker(dockerHost, tlsCertPath string) Invoker {
	host := strings.TrimSpace(dockerHost)
	if host == "" {
		return MockInvoker{}
	}
	return DockerInvoker{
		Host:       host,
		TLSCertDir: strings.TrimSpace(tlsCertPath),
		Fallback:   MockInvoker{},
	}
}

// NewInvokerFromEnv selects MockInvoker when Docker host is unset; otherwise a
// DockerInvoker that attempts nested invoke and soft-fails to mock.
func NewInvokerFromEnv() Invoker {
	return NewInvoker(os.Getenv(EnvDockerHost), os.Getenv(EnvDockerCertPath))
}
