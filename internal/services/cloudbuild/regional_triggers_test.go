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
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/restlab"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudbuild"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestCloudBuildRegionalTriggersHTTP(t *testing.T) {
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
	project := "noctaxris-gcp-local"
	root := "root@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, root); err != nil {
		t.Fatal(err)
	}
	principal := func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: root, IsRoot: true}, true
	}
	mux := http.NewServeMux()
	svc := &cloudbuild.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	loc := "us-central1"
	mux.HandleFunc("POST /v1/projects/{project}/locations/{location}/triggers", restlab.Wrap(principal, svc.CreateTriggerHTTP))
	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/triggers", restlab.Wrap(principal, svc.ListTriggersHTTP))
	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/triggers/{trigger}", restlab.Wrap(principal, func(w http.ResponseWriter, r *http.Request, p authn.Principal) {
		if !svc.GetTriggerHTTP(w, r, p) {
			http.NotFound(w, r)
		}
	}))
	mux.HandleFunc("DELETE /v1/projects/{project}/locations/{location}/triggers/{trigger}", restlab.Wrap(principal, func(w http.ResponseWriter, r *http.Request, p authn.Principal) {
		if !svc.DeleteTriggerHTTP(w, r, p) {
			http.NotFound(w, r)
		}
	}))
	mux.HandleFunc("POST /v1/projects/{project}/locations/{location}/triggers/{trigger}", restlab.Wrap(principal, svc.TriggerPOSTActionHTTP))

	if !svc.MayListTriggers(authn.Principal{Email: root, IsRoot: true}, project) {
		t.Fatal("root may list")
	}
	_ = cloudbuild.TriggerResourceJSON(store.CbTrigger{
		Name: "projects/" + project + "/locations/" + loc + "/triggers/x",
		TriggerID: "x", TriggerJSON: `{"id":"x","filename":"cloudbuild.yaml"}`,
	})

	base := "/v1/projects/" + project + "/locations/" + loc + "/triggers"
	req := httptest.NewRequest(http.MethodPost, base,
		bytes.NewReader([]byte(`{"id":"reg-trig","filename":"cloudbuild.yaml"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base+"/reg-trig", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var list map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list["triggers"].([]any)) < 1 {
		t.Fatalf("list=%#v", list)
	}

	req = httptest.NewRequest(http.MethodPost, base+"/reg-trig:run",
		bytes.NewReader([]byte(`{"branchName":"main"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("run: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, base+"/reg-trig", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, base+"/reg-trig", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get missing: %d", rec.Code)
	}
}
