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

func TestCloudBuildWorkerPoolUseOnHostProject(t *testing.T) {
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
	workload := "noctaxris-gcp-local"
	host := "cb-host"
	root := "root@" + workload + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(workload, root); err != nil {
		t.Fatal(err)
	}
	builder := "builder@" + workload + ".iam.gserviceaccount.com"
	if err := st.CreateServiceAccount(store.ServiceAccount{
		ProjectID: workload, Email: builder, UniqueID: "builder", DisplayName: "builder",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateCustomRole(workload, "buildOnly", "Build", "", "GA", []string{"cloudbuild.builds.create"}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutIAMPolicyJSON("projects/"+workload, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "projects/" + workload + "/roles/buildOnly",
			Members: []string{"serviceAccount:" + builder},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	var who authn.Principal
	who = authn.Principal{Email: root, IsRoot: true}
	mux := http.NewServeMux()
	svc := &cloudbuild.Service{Store: st, Authz: &authz.Evaluator{Policies: st, Roles: st}}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) { return who, true })

	poolBody := `{"displayName":"private","annotations":{"NO_PUBLIC_EGRESS":"true"}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+host+"/locations/us-central1/workerPools?workerPoolId=pool-a",
		bytes.NewReader([]byte(poolBody)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create pool status=%d body=%s", rec.Code, rec.Body.String())
	}
	var pool map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &pool); err != nil {
		t.Fatal(err)
	}
	if pool["state"] != "RUNNING" {
		t.Fatalf("pool %#v", pool)
	}
	ann, _ := pool["annotations"].(map[string]any)
	if ann["NO_PUBLIC_EGRESS"] != "true" {
		t.Fatalf("annotations %#v", ann)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/projects/"+host+"/locations/us-central1/workerPools", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list pools status=%d body=%s", rec.Code, rec.Body.String())
	}

	poolName := "projects/" + host + "/locations/us-central1/workerPools/pool-a"
	buildBody := `{"steps":[{"name":"gcr.io/cloud-builders/gcloud"}],"options":{"pool":{"name":"` + poolName + `"}}}`
	who = authn.Principal{Email: builder, IsRoot: false}
	req = httptest.NewRequest(http.MethodPost, "/v1/projects/"+workload+"/builds", bytes.NewReader([]byte(buildBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing workerpools.use status=%d body=%s", rec.Code, rec.Body.String())
	}

	who = authn.Principal{Email: root, IsRoot: true}
	req = httptest.NewRequest(http.MethodPost, "/v1/projects/"+workload+"/builds", bytes.NewReader([]byte(buildBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("root pool build status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCloudBuildRetryWorkerPoolUseOnHostProject(t *testing.T) {
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
	workload := "noctaxris-gcp-local"
	host := "cb-host"
	root := "root@" + workload + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(workload, root); err != nil {
		t.Fatal(err)
	}
	builder := "builder@" + workload + ".iam.gserviceaccount.com"
	if err := st.CreateServiceAccount(store.ServiceAccount{
		ProjectID: workload, Email: builder, UniqueID: "builder", DisplayName: "builder",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateCustomRole(workload, "buildOnly", "Build", "", "GA", []string{"cloudbuild.builds.create"}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutIAMPolicyJSON("projects/"+workload, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "projects/" + workload + "/roles/buildOnly",
			Members: []string{"serviceAccount:" + builder},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	var who authn.Principal
	who = authn.Principal{Email: root, IsRoot: true}
	mux := http.NewServeMux()
	svc := &cloudbuild.Service{Store: st, Authz: &authz.Evaluator{Policies: st, Roles: st}}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) { return who, true })

	poolBody := `{"displayName":"private","annotations":{"NO_PUBLIC_EGRESS":"true"}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+host+"/locations/us-central1/workerPools?workerPoolId=pool-a",
		bytes.NewReader([]byte(poolBody)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create pool status=%d body=%s", rec.Code, rec.Body.String())
	}

	poolName := "projects/" + host + "/locations/us-central1/workerPools/pool-a"
	buildBody := `{"steps":[{"name":"gcr.io/cloud-builders/gcloud"}],"options":{"pool":{"name":"` + poolName + `"}}}`

	req = httptest.NewRequest(http.MethodPost, "/v1/projects/"+workload+"/builds", bytes.NewReader([]byte(buildBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("global pooled create status=%d body=%s", rec.Code, rec.Body.String())
	}
	globalID := opBuildID(t, rec)

	req = httptest.NewRequest(http.MethodPost, "/v1/projects/"+workload+"/locations/us-central1/builds", bytes.NewReader([]byte(buildBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("regional pooled create status=%d body=%s", rec.Code, rec.Body.String())
	}
	regionalID := opBuildID(t, rec)

	req = httptest.NewRequest(http.MethodPost, "/v1/projects/"+workload+"/builds",
		bytes.NewReader([]byte(`{"steps":[{"name":"gcr.io/cloud-builders/gcloud"}]}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unpooled create status=%d body=%s", rec.Code, rec.Body.String())
	}
	plainID := opBuildID(t, rec)

	ghostID := store.NewCbBuildID()
	ghostName := "projects/" + workload + "/builds/" + ghostID
	ghostJSON := `{"options":{"pool":{"name":"projects/` + host + `/locations/us-central1/workerPools/gone"}}}`
	ok, err := st.CreateCbBuild(store.CbBuild{
		Name: ghostName, ProjectID: workload, Location: "global", BuildID: ghostID,
		Status: "WORKING", BuildJSON: ghostJSON,
	})
	if err != nil || !ok {
		t.Fatalf("ghost pooled build ok=%v err=%v", ok, err)
	}

	who = authn.Principal{Email: builder, IsRoot: false}
	req = httptest.NewRequest(http.MethodPost, "/v1/projects/"+workload+"/builds/"+globalID+":retry", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("global retry without workerpools.use status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/projects/"+workload+"/locations/us-central1/builds/"+regionalID+":retry", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("regional retry without workerpools.use status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/projects/"+workload+"/builds/"+plainID+":retry", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unpooled retry status=%d body=%s", rec.Code, rec.Body.String())
	}

	who = authn.Principal{Email: root, IsRoot: true}
	req = httptest.NewRequest(http.MethodPost, "/v1/projects/"+workload+"/builds/"+ghostID+":retry", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing pool retry status=%d body=%s", rec.Code, rec.Body.String())
	}
	var env struct {
		Error struct {
			Status  string `json:"status"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Status != "FAILED_PRECONDITION" {
		t.Fatalf("missing pool error %#v", env)
	}

	if err := st.PutIAMPolicyJSON("projects/"+host, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "roles/cloudbuild.workerPoolUser",
			Members: []string{"serviceAccount:" + builder},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	who = authn.Principal{Email: builder, IsRoot: false}
	req = httptest.NewRequest(http.MethodPost, "/v1/projects/"+workload+"/builds/"+globalID+":retry", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("retry with workerPoolUser status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := opPoolName(t, rec); got != poolName {
		t.Fatalf("retried pool name %q want %q", got, poolName)
	}
}

func opBuildID(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var op struct {
		Metadata struct {
			Build struct {
				ID string `json:"id"`
			} `json:"build"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &op); err != nil {
		t.Fatal(err)
	}
	if op.Metadata.Build.ID == "" {
		t.Fatalf("missing build id body=%s", rec.Body.String())
	}
	return op.Metadata.Build.ID
}

func opPoolName(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var op map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &op); err != nil {
		t.Fatal(err)
	}
	md, _ := op["metadata"].(map[string]any)
	build, _ := md["build"].(map[string]any)
	opts, _ := build["options"].(map[string]any)
	pool, _ := opts["pool"].(map[string]any)
	name, _ := pool["name"].(string)
	if name == "" {
		t.Fatalf("missing pool name in %#v", op)
	}
	return name
}
