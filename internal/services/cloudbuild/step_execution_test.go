package cloudbuild_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/compute"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/httpegress"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudbuild"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

type holdRunner struct{}

func (holdRunner) Run(context.Context, store.CbBuild) error { return nil }

func setupStepExec(t *testing.T) (*store.Store, *http.ServeMux, *cloudbuild.Service) {
	t.Helper()
	t.Setenv(compute.EnvDockerHost, "")
	t.Setenv(compute.EnvInjectHostGateway, "")
	t.Setenv(httpegress.EnvHTTPEgress, "")
	t.Setenv(httpegress.EnvHTTPAllowlist, "")
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "data"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.EnsureRoot("noctaxris-gcp-local", "root@noctaxris-gcp-local.iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	svc := &cloudbuild.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	})
	return st, mux, svc
}

func postBuild(t *testing.T, mux *http.ServeMux, body string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/noctaxris-gcp-local/builds", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("createBuild status=%d body=%s", rec.Code, rec.Body.String())
	}
	var op map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &op); err != nil {
		t.Fatal(err)
	}
	meta, _ := op["metadata"].(map[string]any)
	build, _ := meta["build"].(map[string]any)
	if build["status"] != "WORKING" {
		t.Fatalf("createBuild status=%#v", build["status"])
	}
	id, _ := build["id"].(string)
	if id == "" {
		t.Fatalf("missing build id: %#v", build)
	}
	return build
}

func getBuild(t *testing.T, mux *http.ServeMux, id string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/projects/noctaxris-gcp-local/builds/"+id, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("getBuild status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestCreateBuildStaysWorkingUntilRunnerFinishes(t *testing.T) {
	st, mux, svc := setupStepExec(t)
	svc.StepRunner = holdRunner{}
	created := postBuild(t, mux, `{"steps":[{"name":"alpine:3.23","args":["true"]}]}`)
	id, _ := created["id"].(string)
	got := getBuild(t, mux, id)
	if got["status"] != "WORKING" {
		t.Fatalf("held runner must leave WORKING: %#v", got)
	}
	name, _ := got["name"].(string)
	b, ok, err := st.GetCbBuild(name)
	if err != nil || !ok {
		t.Fatalf("store get: ok=%v err=%v", ok, err)
	}
	svc.StepRunner = &cloudbuild.EngineRunner{
		Store:       st,
		ExecuteStep: func(context.Context, cloudbuild.BuildStep) error { return nil },
	}
	if err := svc.StepRunner.Run(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	got = getBuild(t, mux, id)
	if got["status"] != "SUCCESS" {
		t.Fatalf("after runner: %#v", got)
	}
}

func TestGetBuildSuccessAfterInjectedRunner(t *testing.T) {
	st, mux, svc := setupStepExec(t)
	svc.StepRunner = &cloudbuild.EngineRunner{
		Store:       st,
		ExecuteStep: func(context.Context, cloudbuild.BuildStep) error { return nil },
	}
	created := postBuild(t, mux, `{"steps":[{"name":"alpine:3.23"}]}`)
	id, _ := created["id"].(string)
	got := getBuild(t, mux, id)
	if got["status"] != "SUCCESS" {
		t.Fatalf("injected runner: %#v", got)
	}
}

func TestMissingEngineGetBuildNotSuccess(t *testing.T) {
	_, mux, _ := setupStepExec(t)
	created := postBuild(t, mux, `{"steps":[{"name":"gcr.io/cloud-builders/gcloud"}]}`)
	id, _ := created["id"].(string)
	got := getBuild(t, mux, id)
	if got["status"] != "WORKING" {
		t.Fatalf("missing engine must stay WORKING: %#v", got)
	}
	detail, _ := got["statusDetail"].(string)
	if detail != "nested engine not configured" {
		t.Fatalf("statusDetail=%q body=%#v", detail, got)
	}
}

func TestBuildSubstitutionsAndAvailableSecretsRoundTrip(t *testing.T) {
	_, mux, _ := setupStepExec(t)
	body := `{
		"steps":[{"name":"alpine:3.23"}],
		"substitutions":{"_REGION":"us-central1","COMMIT_SHA":"abc"},
		"availableSecrets":{"secretManager":[{"versionName":"projects/p/secrets/s/versions/1","env":"TOKEN"}]},
		"logs":{"cloudLogs":{}},
		"logUrl":"http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/builds/custom/logs"
	}`
	created := postBuild(t, mux, body)
	id, _ := created["id"].(string)
	got := getBuild(t, mux, id)
	subs, _ := got["substitutions"].(map[string]any)
	if subs["_REGION"] != "us-central1" || subs["COMMIT_SHA"] != "abc" {
		t.Fatalf("substitutions=%#v", subs)
	}
	secrets, _ := got["availableSecrets"].(map[string]any)
	sm, _ := secrets["secretManager"].([]any)
	if len(sm) != 1 {
		t.Fatalf("availableSecrets=%#v", secrets)
	}
	if got["logUrl"] != "http://127.0.0.1:4588/v1/projects/noctaxris-gcp-local/builds/custom/logs" {
		t.Fatalf("logUrl=%#v", got["logUrl"])
	}
	if _, ok := got["logs"]; !ok {
		t.Fatalf("logs missing: %#v", got)
	}
}
