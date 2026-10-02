package cloudrun

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/compute"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

const (
	// EnvPublicURIBase overrides service.uri when a nested HTTP container is up
	// (for example http://127.0.0.1:8091 when the engine port is host-published).
	EnvPublicURIBase = "NOCTAXRIS_GCP_RUN_PUBLIC_URI_BASE"

	defaultPublicURIBase = "http://127.0.0.1:4588"
	runProxyPrefix       = "/run/"
	maxProxyBodyBytes    = 32 << 20
)

// NestedRunner is the nested engine surface used for long-lived Cloud Run HTTP.
// *compute.Client implements it. Tests inject fakes.
type NestedRunner interface {
	Enabled() bool
	StartRunHTTPWithOptions(ctx context.Context, opts compute.RunHTTPOptions) (compute.RunHTTPResult, error)
	RemoveRunHTTP(ctx context.Context, containerID string) error
}

var nestedTransport = &http.Transport{
	Proxy:                 nil,
	DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
	ResponseHeaderTimeout: 60 * time.Second,
	MaxIdleConnsPerHost:   4,
	IdleConnTimeout:       30 * time.Second,
}

func (s *Service) engineEnabled() bool {
	return s.Engine != nil && s.Engine.Enabled()
}

// mountRunProxy registers the browser-facing nested HTTP route. Middleware may
// skip required Bearer (IsPublicPath), but Invoker is still enforced here
// (principal or allUsers). Only services with a running nested container are
// reachable. :invoke keeps the same IAM check.
func (s *Service) mountRunProxy(mux *http.ServeMux, principalFrom principalFunc) {
	mux.HandleFunc(runProxyPrefix+"{project}/{location}/{service}/{path...}", func(w http.ResponseWriter, r *http.Request) {
		s.handleRunProxy(w, r, principalFrom)
	})
	mux.HandleFunc(runProxyPrefix+"{project}/{location}/{service}", func(w http.ResponseWriter, r *http.Request) {
		target := r.URL.Path + "/"
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target, http.StatusPermanentRedirect)
	})
}

func (s *Service) handleRunProxy(w http.ResponseWriter, r *http.Request, principalFrom principalFunc) {
	project := r.PathValue("project")
	name := serviceName(project, r.PathValue("location"), r.PathValue("service"))
	if err := s.requireRunInvoker(r, principalFrom, name, project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	svc, ok, err := s.Store.GetRunService(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Service not found")
		return
	}
	s.proxyNested(w, r, svc, r.PathValue("path"))
}

// requireRunInvoker requires run.routes.invoke on the service or project.
// Authenticated callers use EvaluateAny; anonymous callers need allUsers Invoker.
func (s *Service) requireRunInvoker(r *http.Request, principalFrom principalFunc, name, project string) error {
	var p authn.Principal
	var hasP bool
	if principalFrom != nil {
		p, hasP = principalFrom(r)
	}
	ok, err := s.Authz.AllowPrincipalOrAllUsers(p.Email, p.IsRoot, hasP, "run.routes.invoke", name, "projects/"+project)
	if err != nil {
		return err
	}
	if !ok {
		return errDenied
	}
	return nil
}

// proxyNested reverse-proxies r to the service nested container at /{path}.
func (s *Service) proxyNested(w http.ResponseWriter, r *http.Request, svc store.RunService, path string) {
	hostport, ok := nestedHostPort(svc)
	if !ok {
		gcperrors.NotFound(w, "Service not found")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxProxyBodyBytes)
	newNestedProxy(hostport, path).ServeHTTP(w, r)
}

func newNestedProxy(hostport, path string) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = hostport
			pr.Out.URL.Path = "/" + strings.TrimPrefix(path, "/")
			pr.Out.URL.RawPath = ""
			pr.Out.Host = ""
			// The lab Bearer authenticates the emulator API, never the nested app.
			pr.Out.Header.Del("Authorization")
			pr.Out.Header.Del("Proxy-Authorization")
			pr.SetXForwarded()
		},
		Transport: nestedTransport,
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			gcperrors.WriteREST(w, http.StatusBadGateway, gcperrors.StatusUnavailable,
				"nested Cloud Run container unreachable")
		},
	}
}

// nestedHostPort returns the engine dial target when a long-lived container backs svc.
// Deterministic labResponseBody fixtures stay mock-only.
func nestedHostPort(svc store.RunService) (string, bool) {
	if svc.LabResponseBody != "" || svc.NestedHost == "" || svc.NestedPort <= 0 {
		return "", false
	}
	return net.JoinHostPort(svc.NestedHost, strconv.Itoa(svc.NestedPort)), true
}

// normalizePublicURIBase returns a trimmed http(s) base URL or "" when invalid.
func normalizePublicURIBase(raw string) string {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	if base == "" {
		return ""
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return base
}

// nestedServiceURI picks service.uri for a running nested container. A valid
// override base maps straight to the host-published container. Otherwise the
// emulator /run/ route is used.
func nestedServiceURI(project, location, serviceID, overrideBase string) string {
	if base := normalizePublicURIBase(overrideBase); base != "" {
		return base + "/"
	}
	return fmt.Sprintf("%s%s%s/%s/%s/", defaultPublicURIBase, runProxyPrefix, project, location, serviceID)
}

// primaryContainer returns the first container map that declares an image.
func primaryContainer(tpl map[string]any) map[string]any {
	for _, src := range []map[string]any{tpl, nestedTemplate(tpl)} {
		containers, _ := src["containers"].([]any)
		for _, c := range containers {
			cm, _ := c.(map[string]any)
			if img, _ := cm["image"].(string); img != "" {
				return cm
			}
		}
	}
	return nil
}

func nestedTemplate(tpl map[string]any) map[string]any {
	inner, _ := tpl["template"].(map[string]any)
	return inner
}

func stringList(v any) []string {
	items, _ := v.([]any)
	var out []string
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// runHTTPOptions builds nested start options when svc should run as a long-lived
// container: it has a container image, no labResponseBody, and declares a
// container port, command, or args.
func runHTTPOptions(svc store.RunService, tpl map[string]any) (compute.RunHTTPOptions, bool) {
	if svc.LabResponseBody != "" {
		return compute.RunHTTPOptions{}, false
	}
	c := primaryContainer(tpl)
	if c == nil {
		return compute.RunHTTPOptions{}, false
	}
	image, _ := c["image"].(string)
	port := compute.DefaultRunContainerPort
	ports, _ := c["ports"].([]any)
	if len(ports) > 0 {
		pm, _ := ports[0].(map[string]any)
		if n, ok := asInt(pm["containerPort"]); ok && n > 0 {
			port = n
		}
	}
	command, args := stringList(c["command"]), stringList(c["args"])
	// An image alone keeps the one-shot nested :invoke path. A declared port,
	// command, or args marks the container as a long-lived HTTP server.
	if len(ports) == 0 && len(command) == 0 && len(args) == 0 {
		return compute.RunHTTPOptions{}, false
	}
	env := map[string]string{}
	envs, _ := c["env"].([]any)
	for _, e := range envs {
		em, _ := e.(map[string]any)
		name, _ := em["name"].(string)
		val, _ := em["value"].(string)
		if name != "" {
			env[name] = val
		}
	}
	if _, ok := env["PORT"]; !ok {
		env["PORT"] = strconv.Itoa(port)
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		pairs = append(pairs, k+"="+env[k])
	}
	return compute.RunHTTPOptions{
		Image:         image,
		Name:          compute.RunHTTPContainerName(svc.ProjectID, svc.Location, svc.ServiceID),
		Env:           pairs,
		ContainerPort: port,
		Entrypoint:    command,
		Cmd:           args,
	}, true
}

// reconcileNested replaces any prior nested container for svc and starts a new
// one when the template qualifies. Soft failures leave the mock URI in place
// unless NOCTAXRIS_GCP_NESTED_ENGINE_FAIL_CLOSED is set, in which case the
// error is returned.
func (s *Service) reconcileNested(ctx context.Context, svc store.RunService) (store.RunService, error) {
	if !s.engineEnabled() {
		return svc, nil
	}
	var tpl map[string]any
	if err := json.Unmarshal([]byte(svc.TemplateJSON), &tpl); err != nil {
		return svc, fmt.Errorf("parse service template: %w", err)
	}
	if svc.ContainerID != "" {
		// Best-effort: the replacement start also force-removes by stable name.
		_ = s.Engine.RemoveRunHTTP(ctx, svc.ContainerID)
	}
	opts, eligible := runHTTPOptions(svc, tpl)
	if !eligible {
		if svc.ContainerID == "" && svc.NestedHost == "" {
			return svc, nil
		}
		return s.recordNested(svc, store.DefaultRunServiceURI(svc.Name), "", 0, "")
	}
	res, err := s.Engine.StartRunHTTPWithOptions(ctx, opts)
	if err != nil {
		cleared, recErr := s.recordNested(svc, store.DefaultRunServiceURI(svc.Name), "", 0, "")
		if recErr != nil {
			return svc, recErr
		}
		if compute.NestedEngineFailClosed() {
			return cleared, err
		}
		return cleared, nil
	}
	uri := nestedServiceURI(svc.ProjectID, svc.Location, svc.ServiceID, os.Getenv(EnvPublicURIBase))
	return s.recordNested(svc, uri, res.EngineHost, res.HostPort, res.ContainerID)
}

func (s *Service) recordNested(svc store.RunService, uri, host string, port int, containerID string) (store.RunService, error) {
	ok, err := s.Store.UpdateRunServiceNested(svc.Name, uri, host, port, containerID)
	if err != nil {
		return svc, err
	}
	if !ok {
		return svc, fmt.Errorf("run service %s disappeared during nested start", svc.Name)
	}
	svc.URI, svc.NestedHost, svc.NestedPort, svc.ContainerID = uri, host, port, containerID
	return svc, nil
}

// removeNested force-removes the nested container for a deleted service.
func (s *Service) removeNested(ctx context.Context, svc store.RunService) {
	if svc.ContainerID == "" || !s.engineEnabled() {
		return
	}
	_ = s.Engine.RemoveRunHTTP(ctx, svc.ContainerID)
}

// writeNestedFailure reports a fail-closed nested start error. When rollback is
// set the newly created service row is deleted so create is not half-applied.
func (s *Service) writeNestedFailure(w http.ResponseWriter, err error, rollbackName string) {
	if rollbackName != "" {
		_, _ = s.Store.DeleteRunService(rollbackName)
	}
	gcperrors.WriteREST(w, http.StatusBadRequest, gcperrors.StatusFailedPrecondition, compute.NestedEngineFailClosedMessage(err))
}

// invokeNested proxies :invoke to the nested container (path "/"), keeping IAM
// on the invoke route. The request body was already read for last-invoke theatre.
func (s *Service) invokeNested(w http.ResponseWriter, r *http.Request, svc store.RunService, body []byte) bool {
	if _, ok := nestedHostPort(svc); !ok {
		return false
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	s.proxyNested(w, r, svc, "")
	return true
}
