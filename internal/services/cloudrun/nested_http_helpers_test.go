package cloudrun

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/compute"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestNestedServiceURISelection(t *testing.T) {
	t.Parallel()
	got := nestedServiceURI("p", "us-central1", "web", "")
	if got != "http://127.0.0.1:4588/run/p/us-central1/web/" {
		t.Fatalf("default uri=%q", got)
	}
	if got := nestedServiceURI("p", "us-central1", "web", "http://127.0.0.1:8091/"); got != "http://127.0.0.1:8091/" {
		t.Fatalf("override uri=%q", got)
	}
	for _, bad := range []string{"ftp://x", "not a url", "127.0.0.1:8091", "http://"} {
		if got := nestedServiceURI("p", "us-central1", "web", bad); got != "http://127.0.0.1:4588/run/p/us-central1/web/" {
			t.Fatalf("invalid base %q should fall back, got %q", bad, got)
		}
	}
}

func TestRunHTTPOptionsEligibility(t *testing.T) {
	t.Parallel()
	svc := store.RunService{Name: "n", ProjectID: "p", Location: "l", ServiceID: "s"}
	parse := func(raw string) map[string]any {
		var tpl map[string]any
		if err := json.Unmarshal([]byte(raw), &tpl); err != nil {
			t.Fatal(err)
		}
		return tpl
	}

	if _, ok := runHTTPOptions(svc, parse(`{"containers":[{"image":"alpine:3.20"}]}`)); ok {
		t.Fatal("image alone must keep one-shot invoke")
	}
	if _, ok := runHTTPOptions(svc, parse(`{}`)); ok {
		t.Fatal("no image must not start")
	}
	lab := svc
	lab.LabResponseBody = `{"ok":true}`
	if _, ok := runHTTPOptions(lab, parse(`{"containers":[{"image":"python:3.13-slim-bookworm","ports":[{"containerPort":8000}]}]}`)); ok {
		t.Fatal("labResponseBody must force mock")
	}

	opts, ok := runHTTPOptions(svc, parse(`{"containers":[{
		"image":"python:3.13-slim-bookworm",
		"command":["python"],
		"args":["-m","http.server","9000"],
		"ports":[{"containerPort":9000}],
		"env":[{"name":"B","value":"2"},{"name":"A","value":"1"}]
	}]}`))
	if !ok {
		t.Fatal("expected eligible")
	}
	if opts.Image != "python:3.13-slim-bookworm" || opts.ContainerPort != 9000 {
		t.Fatalf("opts=%#v", opts)
	}
	wantEnv := []string{"A=1", "B=2", "PORT=9000"}
	if len(opts.Env) != len(wantEnv) {
		t.Fatalf("env=%v", opts.Env)
	}
	for i := range wantEnv {
		if opts.Env[i] != wantEnv[i] {
			t.Fatalf("env=%v want %v", opts.Env, wantEnv)
		}
	}
	if len(opts.Entrypoint) != 1 || opts.Entrypoint[0] != "python" || len(opts.Cmd) != 3 {
		t.Fatalf("command/args=%v %v", opts.Entrypoint, opts.Cmd)
	}
	if opts.Name != compute.RunHTTPContainerName("p", "l", "s") {
		t.Fatalf("name=%q", opts.Name)
	}

	def, ok := runHTTPOptions(svc, parse(`{"template":{"containers":[{"image":"python:3.13-slim-bookworm","args":["x"],"env":[{"name":"PORT","value":"7000"}]}]}}`))
	if !ok || def.ContainerPort != compute.DefaultRunContainerPort {
		t.Fatalf("default port opts=%#v ok=%v", def, ok)
	}
	if len(def.Env) != 1 || def.Env[0] != "PORT=7000" {
		t.Fatalf("explicit PORT must be kept: %v", def.Env)
	}
}

func TestNestedHostPort(t *testing.T) {
	t.Parallel()
	if _, ok := nestedHostPort(store.RunService{}); ok {
		t.Fatal("empty service has no nested target")
	}
	if _, ok := nestedHostPort(store.RunService{NestedHost: "engine", NestedPort: 0}); ok {
		t.Fatal("zero port has no nested target")
	}
	if _, ok := nestedHostPort(store.RunService{NestedHost: "engine", NestedPort: 8080, LabResponseBody: "x"}); ok {
		t.Fatal("labResponseBody forces mock")
	}
	hp, ok := nestedHostPort(store.RunService{NestedHost: "engine", NestedPort: 49153})
	if !ok || hp != "engine:49153" {
		t.Fatalf("hostport=%q ok=%v", hp, ok)
	}
}

func TestNestedProxyRoutingStripsAuthorization(t *testing.T) {
	t.Parallel()
	var gotPath, gotAuth, gotQuery, gotFwd string
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotAuth = r.Header.Get("Authorization")
		gotFwd = r.Header.Get("X-Forwarded-For")
		_, _ = w.Write([]byte("backend-ok"))
	}))
	defer backend.Close()

	hostport := backend.Listener.Addr().String()
	req := httptest.NewRequest(http.MethodGet, "/run/p/l/s/api/items?x=1", nil)
	req.Header.Set("Authorization", "Bearer lab-secret")
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	rec := httptest.NewRecorder()
	newNestedProxy(hostport, "api/items").ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "backend-ok" {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
	if gotPath != "/api/items" || gotQuery != "x=1" {
		t.Fatalf("path=%q query=%q", gotPath, gotQuery)
	}
	if gotAuth != "" {
		t.Fatalf("Authorization must not reach the nested app, got %q", gotAuth)
	}
	if gotFwd == "203.0.113.9" {
		t.Fatalf("client X-Forwarded-For must not be trusted, got %q", gotFwd)
	}

	rec = httptest.NewRecorder()
	newNestedProxy(hostport, "").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/run/p/l/s/", nil))
	if gotPath != "/" {
		t.Fatalf("root path=%q", gotPath)
	}
}

func TestNestedProxyUnreachableReturns502WithoutLeakingTarget(t *testing.T) {
	t.Parallel()
	backend := httptest.NewServer(http.NotFoundHandler())
	hostport := backend.Listener.Addr().String()
	backend.Close()
	rec := httptest.NewRecorder()
	newNestedProxy(hostport, "").ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d", rec.Code)
	}
	if body := rec.Body.String(); body == "" || strings.Contains(body, hostport) {
		t.Fatalf("body must not leak engine target: %q", body)
	}
}
