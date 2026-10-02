package cloudrun_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/compute"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/binaryauthorization"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudrun"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

type fakeEngine struct {
	result  compute.RunHTTPResult
	err     error
	started []compute.RunHTTPOptions
	removed []string
}

func (f *fakeEngine) Enabled() bool { return true }

func (f *fakeEngine) StartRunHTTPWithOptions(_ context.Context, opts compute.RunHTTPOptions) (compute.RunHTTPResult, error) {
	f.started = append(f.started, opts)
	if f.err != nil {
		return compute.RunHTTPResult{}, f.err
	}
	return f.result, nil
}

func (f *fakeEngine) RemoveRunHTTP(_ context.Context, containerID string) error {
	f.removed = append(f.removed, containerID)
	return nil
}

func mountNestedCloudRun(t *testing.T, engine cloudrun.NestedRunner) (*http.ServeMux, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "secrets", "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "data"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	const project = "noctaxris-gcp-local"
	root := "root@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, root); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	svc := &cloudrun.Service{
		Store:   st,
		Authz:   &authz.Evaluator{Policies: st},
		Invoker: compute.MockInvoker{},
		Engine:  engine,
	}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: root, IsRoot: true}, true
	})
	return mux, st
}

func nestedBackend(t *testing.T) (*httptest.Server, compute.RunHTTPResult) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Seen-Path", r.URL.Path)
		w.Header().Set("X-Seen-Auth", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte("hello-from-nested"))
	}))
	t.Cleanup(srv.Close)
	host, portStr, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}
	return srv, compute.RunHTTPResult{ContainerID: "ctr-1", HostPort: port, ContainerPort: 8080, EngineHost: host}
}

const nestedTemplateBody = `{"template":{"containers":[{"image":"python:3.13-slim-bookworm","args":["-m","http.server","8080"],"ports":[{"containerPort":8080}]}]}}`

func doJSON(mux http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestNestedRunCreateSetsURIAndServesPublicRoute(t *testing.T) {
	t.Setenv(cloudrun.EnvPublicURIBase, "")
	_, res := nestedBackend(t)
	eng := &fakeEngine{result: res}
	mux, st := mountNestedCloudRun(t, eng)
	base := "/v2/projects/noctaxris-gcp-local/locations/us-central1/services"

	rec := doJSON(mux, http.MethodPost, base+"?serviceId=web", nestedTemplateBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var op map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &op); err != nil {
		t.Fatal(err)
	}
	if op["done"] != true {
		t.Fatalf("expected done operation: %#v", op)
	}
	resp, _ := op["response"].(map[string]any)
	const wantURI = "http://127.0.0.1:4588/run/noctaxris-gcp-local/us-central1/web/"
	if resp["uri"] != wantURI {
		t.Fatalf("uri=%v want %s", resp["uri"], wantURI)
	}
	if len(eng.started) != 1 || eng.started[0].ContainerPort != 8080 {
		t.Fatalf("started=%#v", eng.started)
	}
	stored, ok, err := st.GetRunService("projects/noctaxris-gcp-local/locations/us-central1/services/web")
	if err != nil || !ok {
		t.Fatalf("get: %v %v", ok, err)
	}
	if stored.ContainerID != "ctr-1" || stored.NestedHost != res.EngineHost || stored.NestedPort != res.HostPort {
		t.Fatalf("nested row=%#v", stored)
	}

	name := "projects/noctaxris-gcp-local/locations/us-central1/services/web"
	if err := st.PutIAMPolicyJSON(name, authz.Policy{
		Bindings: []authz.Binding{{Role: "roles/run.invoker", Members: []string{"allUsers"}}},
	}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/run/noctaxris-gcp-local/us-central1/web/some/page", nil)
	req.Header.Set("Authorization", "Bearer lab-secret")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "hello-from-nested" {
		t.Fatalf("proxy status=%d body=%q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Seen-Path") != "/some/page" {
		t.Fatalf("path=%q", rec.Header().Get("X-Seen-Path"))
	}
	if rec.Header().Get("X-Seen-Auth") != "" {
		t.Fatal("Authorization leaked to nested app")
	}

	rec = doJSON(mux, http.MethodGet, "/run/noctaxris-gcp-local/us-central1/web", "")
	if rec.Code != http.StatusPermanentRedirect || rec.Header().Get("Location") != "/run/noctaxris-gcp-local/us-central1/web/" {
		t.Fatalf("redirect status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}

	rec = doJSON(mux, http.MethodPost, base+"/web:invoke", `{"k":"v"}`)
	if rec.Code != http.StatusOK || rec.Body.String() != "hello-from-nested" {
		t.Fatalf("invoke status=%d body=%q", rec.Code, rec.Body.String())
	}

	rec = doJSON(mux, http.MethodDelete, base+"/web", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d", rec.Code)
	}
	if len(eng.removed) == 0 || eng.removed[len(eng.removed)-1] != "ctr-1" {
		t.Fatalf("removed=%v", eng.removed)
	}
	rec = doJSON(mux, http.MethodGet, "/run/noctaxris-gcp-local/us-central1/web/", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("after delete status=%d", rec.Code)
	}
}

func TestNestedRunPublicURIBaseOverride(t *testing.T) {
	t.Setenv(cloudrun.EnvPublicURIBase, "http://127.0.0.1:8091")
	_, res := nestedBackend(t)
	mux, _ := mountNestedCloudRun(t, &fakeEngine{result: res})
	rec := doJSON(mux, http.MethodPost,
		"/v2/projects/noctaxris-gcp-local/locations/us-central1/services?serviceId=web", nestedTemplateBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var op map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &op)
	resp, _ := op["response"].(map[string]any)
	if resp["uri"] != "http://127.0.0.1:8091/" {
		t.Fatalf("uri=%v", resp["uri"])
	}
}

func TestNestedRunPatchReplacesContainer(t *testing.T) {
	t.Setenv(cloudrun.EnvPublicURIBase, "")
	_, res := nestedBackend(t)
	eng := &fakeEngine{result: res}
	mux, _ := mountNestedCloudRun(t, eng)
	base := "/v2/projects/noctaxris-gcp-local/locations/us-central1/services"
	if rec := doJSON(mux, http.MethodPost, base+"?serviceId=web", nestedTemplateBody); rec.Code != http.StatusOK {
		t.Fatalf("create=%d %s", rec.Code, rec.Body.String())
	}
	rec := doJSON(mux, http.MethodPatch, base+"/web", nestedTemplateBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch=%d %s", rec.Code, rec.Body.String())
	}
	if len(eng.started) != 2 {
		t.Fatalf("expected restart on patch, started=%d", len(eng.started))
	}
	if len(eng.removed) != 1 || eng.removed[0] != "ctr-1" {
		t.Fatalf("prior container must be removed, removed=%v", eng.removed)
	}
}

func TestNestedRunSkippedForLabResponseBodyAndImageOnly(t *testing.T) {
	t.Setenv(cloudrun.EnvPublicURIBase, "")
	_, res := nestedBackend(t)
	eng := &fakeEngine{result: res}
	mux, _ := mountNestedCloudRun(t, eng)
	base := "/v2/projects/noctaxris-gcp-local/locations/us-central1/services"

	lab := `{"template":{"containers":[{"image":"python:3.13-slim-bookworm","ports":[{"containerPort":8080}]}],"labResponseBody":"{\"ok\":true}"}}`
	if rec := doJSON(mux, http.MethodPost, base+"?serviceId=lab", lab); rec.Code != http.StatusOK {
		t.Fatalf("lab create=%d %s", rec.Code, rec.Body.String())
	}
	imageOnly := `{"template":{"containers":[{"image":"alpine:3.20"}]}}`
	if rec := doJSON(mux, http.MethodPost, base+"?serviceId=oneshot", imageOnly); rec.Code != http.StatusOK {
		t.Fatalf("image-only create=%d %s", rec.Code, rec.Body.String())
	}
	if len(eng.started) != 0 {
		t.Fatalf("no nested start expected, got %#v", eng.started)
	}
	rec := doJSON(mux, http.MethodGet, "/run/noctaxris-gcp-local/us-central1/lab/", "")
	// Root test principal passes Invoker; labResponseBody has no nested target → 404.
	if rec.Code != http.StatusNotFound {
		t.Fatalf("public route without nested must 404, got %d", rec.Code)
	}
}

func TestNestedRunStartFailureSoftAndFailClosed(t *testing.T) {
	t.Setenv(cloudrun.EnvPublicURIBase, "")
	eng := &fakeEngine{err: errors.New("engine boom")}
	mux, st := mountNestedCloudRun(t, eng)
	base := "/v2/projects/noctaxris-gcp-local/locations/us-central1/services"

	t.Setenv(compute.EnvNestedEngineFailClosed, "")
	rec := doJSON(mux, http.MethodPost, base+"?serviceId=soft", nestedTemplateBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("soft create=%d %s", rec.Code, rec.Body.String())
	}
	var op map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &op)
	resp, _ := op["response"].(map[string]any)
	if uri, _ := resp["uri"].(string); uri != store.DefaultRunServiceURI("projects/noctaxris-gcp-local/locations/us-central1/services/soft") {
		t.Fatalf("soft-fail uri=%q", uri)
	}

	t.Setenv(compute.EnvNestedEngineFailClosed, "1")
	rec = doJSON(mux, http.MethodPost, base+"?serviceId=closed", nestedTemplateBody)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("fail-closed create=%d %s", rec.Code, rec.Body.String())
	}
	if _, ok, err := st.GetRunService("projects/noctaxris-gcp-local/locations/us-central1/services/closed"); err != nil || ok {
		t.Fatalf("fail-closed create must roll back the row: ok=%v err=%v", ok, err)
	}
}

func TestNestedRunBinaryAuthorizationStillConsultedOnCreate(t *testing.T) {
	t.Setenv(cloudrun.EnvPublicURIBase, "")
	_, res := nestedBackend(t)
	eng := &fakeEngine{result: res}
	mux, st := mountNestedCloudRun(t, eng)
	const project = "noctaxris-gcp-local"
	who := func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@" + project + ".iam.gserviceaccount.com", IsRoot: true}, true
	}
	eval := &authz.Evaluator{Policies: st}
	(&binaryauthorization.Service{Store: st, Authz: eval}).Mount(mux, who)
	putEnforcedBinauthzPolicy(t, mux, project)

	base := "/v2/projects/" + project + "/locations/us-central1/services"
	rec := doJSON(mux, http.MethodPost, base+"?serviceId=blocked", nestedTemplateBody)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("denied create status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(eng.started) != 0 {
		t.Fatalf("engine must not start before admission: %#v", eng.started)
	}
}
