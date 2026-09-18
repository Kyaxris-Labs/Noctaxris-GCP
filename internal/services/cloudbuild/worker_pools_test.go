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
