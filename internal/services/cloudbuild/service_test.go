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

func TestCloudBuildDeepenCancelRetryTriggerRun(t *testing.T) {
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

	project := "noctaxris-gcp-local"
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/builds",
		bytes.NewReader([]byte(`{"steps":[{"name":"gcr.io/cloud-builders/docker"}]}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("createBuild status=%d body=%s", rec.Code, rec.Body.String())
	}
	var op map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &op)
	meta, _ := op["metadata"].(map[string]any)
	build, _ := meta["build"].(map[string]any)
	buildID, _ := build["id"].(string)
	if buildID == "" {
		t.Fatalf("missing build id: %#v", build)
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/builds/"+buildID+":cancel",
		bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel status=%d body=%s", rec.Code, rec.Body.String())
	}
	var cancelled map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &cancelled)
	if cancelled["status"] != "CANCELLED" {
		t.Fatalf("cancel build=%#v", cancelled)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/builds/"+buildID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get cancelled status=%d body=%s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &cancelled)
	if cancelled["status"] != "CANCELLED" {
		t.Fatalf("get should stay CANCELLED: %#v", cancelled)
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/builds/"+buildID+":retry",
		bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("retry status=%d body=%s", rec.Code, rec.Body.String())
	}
	var retryOp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &retryOp)
	retryMeta, _ := retryOp["metadata"].(map[string]any)
	retryBuild, _ := retryMeta["build"].(map[string]any)
	if retryBuild["status"] != "WORKING" {
		t.Fatalf("retry build=%#v", retryBuild)
	}
	if retryBuild["id"] == buildID {
		t.Fatal("retry should allocate a new build id")
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/builds",
		bytes.NewReader([]byte(`{"steps":[{"name":"gcr.io/cloud-builders/gcloud","args":["version"]},{"name":"gcr.io/cloud-builders/docker"}]}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	_ = json.Unmarshal(rec.Body.Bytes(), &op)
	meta, _ = op["metadata"].(map[string]any)
	build, _ = meta["build"].(map[string]any)
	stepsID, _ := build["id"].(string)
	req = httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/builds/"+stepsID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var success map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &success)
	if success["status"] != "SUCCESS" {
		t.Fatalf("getBuild=%#v", success)
	}
	steps, _ := success["steps"].([]any)
	if len(steps) != 2 {
		t.Fatalf("steps=%#v", steps)
	}
	for i, s := range steps {
		sm, _ := s.(map[string]any)
		if sm["status"] != "SUCCESS" {
			t.Fatalf("step[%d]=%#v", i, sm)
		}
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/triggers",
		bytes.NewReader([]byte(`{"id":"run-me","filename":"cloudbuild.yaml"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create trigger status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/triggers/run-me:run",
		bytes.NewReader([]byte(`{"branchName":"main"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("trigger run status=%d body=%s", rec.Code, rec.Body.String())
	}
	var runOp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &runOp)
	runMeta, _ := runOp["metadata"].(map[string]any)
	runBuild, _ := runMeta["build"].(map[string]any)
	if runBuild["status"] != "WORKING" {
		t.Fatalf("trigger run build=%#v", runBuild)
	}
	detail, _ := runBuild["statusDetail"].(string)
	if detail == "" {
		t.Fatalf("expected statusDetail on trigger run: %#v", runBuild)
	}
}

func TestCloudBuildTriggersCRUD(t *testing.T) {
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
	project := "noctaxris-gcp-local"
	trigBase := "/v1/projects/" + project + "/triggers"

	req := httptest.NewRequest(http.MethodPost, trigBase,
		bytes.NewReader([]byte(`{"id":"tf-crud","filename":"cloudbuild.yaml"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create trigger status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, trigBase+"/tf-crud", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get trigger status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, trigBase, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list triggers status=%d body=%s", rec.Code, rec.Body.String())
	}
	var list map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	triggers, _ := list["triggers"].([]any)
	if len(triggers) != 1 {
		t.Fatalf("triggers=%#v", list)
	}

	req = httptest.NewRequest(http.MethodDelete, trigBase+"/tf-crud", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete trigger status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCloudBuildFailClosed(t *testing.T) {
	mux := http.NewServeMux()
	svc := &cloudbuild.Service{}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) { return authn.Principal{}, false })
	req := httptest.NewRequest(http.MethodGet, "/v1/projects/p/builds", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestCloudBuildAuthzDenyNonRootWithoutBinding(t *testing.T) {
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
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}
