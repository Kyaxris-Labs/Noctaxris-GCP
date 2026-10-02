package cloudbuild_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudbuild"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func mountCloudBuild(t *testing.T) (*http.ServeMux, *store.Store) {
	t.Helper()
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
	svc := &cloudbuild.Service{Store: st, Authz: &authz.Evaluator{Policies: st}, StepRunner: holdRunner{}}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	})
	return mux, st
}

func TestCloudBuildRegionalLogsGetCancelRetryAndWorkerPoolGet(t *testing.T) {
	mux, st := mountCloudBuild(t)
	project := "noctaxris-gcp-local"
	loc := "us-central1"
	base := "/v1/projects/" + project + "/locations/" + loc + "/builds"

	req := httptest.NewRequest(http.MethodPost, base, bytes.NewReader([]byte(`{"steps":[{"name":"gcr.io/cloud-builders/gcloud"}]}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var op map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &op)
	meta, _ := op["metadata"].(map[string]any)
	build, _ := meta["build"].(map[string]any)
	buildID, _ := build["id"].(string)
	name, _ := build["name"].(string)
	if buildID == "" || name == "" {
		t.Fatalf("build=%#v", build)
	}
	if err := st.PutCbBuildLogs(name, "step log line\n"); err != nil {
		t.Fatal(err)
	}

	req = httptest.NewRequest(http.MethodGet, base+"/"+buildID+"/logs", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte("step log line")) {
		t.Fatalf("regional logs: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/builds/"+buildID+"/logs", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("global logs by id: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base+"/missing-build/logs", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing logs: %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, base+"/missing-build", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing get: %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, base+"/"+buildID+":cancel", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, base+"/missing:cancel", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cancel missing: %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, base+"/"+buildID+":retry", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("retry: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, base+"/"+buildID+":nope", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown action: %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/triggers/missing", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing trigger: %d", rec.Code)
	}

	poolBase := "/v1/projects/" + project + "/locations/" + loc + "/workerPools"
	req = httptest.NewRequest(http.MethodPost, poolBase+"?workerPoolId=pool-logs",
		bytes.NewReader([]byte(`{"displayName":"p"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create pool: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, poolBase+"/pool-logs", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get pool: %d %s", rec.Code, rec.Body.String())
	}
	var pool map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &pool)
	if pool["name"] == nil {
		t.Fatalf("pool=%#v", pool)
	}

	req = httptest.NewRequest(http.MethodGet, poolBase+"/missing-pool", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing pool: %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, poolBase, bytes.NewReader([]byte(`{}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("pool without id: %d", rec.Code)
	}
}

func TestCloudBuildAuthzDenyAndUnauthenticated(t *testing.T) {
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
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/projects/noctaxris-gcp-local/builds", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("deny: %d", rec.Code)
	}

	mux2 := http.NewServeMux()
	svc.Mount(mux2, func(*http.Request) (authn.Principal, bool) { return authn.Principal{}, false })
	req = httptest.NewRequest(http.MethodGet, "/v1/projects/noctaxris-gcp-local/builds", nil)
	rec = httptest.NewRecorder()
	mux2.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth: %d", rec.Code)
	}
}
