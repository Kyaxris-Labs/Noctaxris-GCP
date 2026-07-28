package resourcemanager_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/resourcemanager"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func openCRM(t *testing.T) (*http.ServeMux, *store.Store) {
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
	h := &resourcemanager.Handler{
		Store: st,
		Authz: &authz.Evaluator{Policies: st},
		Principal: func(*http.Request) (authn.Principal, bool) {
			return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
		},
	}
	h.Mount(mux)
	return mux, st
}

func TestCRMMoveUndeleteSearchOrgIAMAndLabels(t *testing.T) {
	mux, _ := openCRM(t)

	createA := []byte(`{"parent":"` + store.DefaultOrganizationName + `","displayName":"Team A"}`)
	req := httptest.NewRequest(http.MethodPost, "/v3/folders", bytes.NewReader(createA))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create A status=%d body=%s", rec.Code, rec.Body.String())
	}
	var folderA map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &folderA)
	idA := folderA["name"].(string)[len("folders/"):]

	createB := []byte(`{"parent":"` + store.DefaultOrganizationName + `","displayName":"Team B"}`)
	req = httptest.NewRequest(http.MethodPost, "/v3/folders", bytes.NewReader(createB))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create B status=%d body=%s", rec.Code, rec.Body.String())
	}
	var folderB map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &folderB)
	idB := folderB["name"].(string)[len("folders/"):]

	moveBody := []byte(`{"destinationParent":"folders/` + idB + `"}`)
	req = httptest.NewRequest(http.MethodPost, "/v3/folders/"+idA+":move", bytes.NewReader(moveBody))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("move status=%d body=%s", rec.Code, rec.Body.String())
	}
	var moved map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &moved)
	if moved["parent"] != "folders/"+idB {
		t.Fatalf("moved parent = %#v", moved["parent"])
	}

	req = httptest.NewRequest(http.MethodGet, "/v3/folders:search?query=displayName=Team", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("search status=%d body=%s", rec.Code, rec.Body.String())
	}
	var search map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &search)
	folders, _ := search["folders"].([]any)
	if len(folders) < 2 {
		t.Fatalf("search = %#v", search)
	}

	req = httptest.NewRequest(http.MethodDelete, "/v3/folders/"+idA, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v3/folders/"+idA+":undelete", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("undelete status=%d body=%s", rec.Code, rec.Body.String())
	}
	var undel map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &undel)
	if undel["state"] != "ACTIVE" {
		t.Fatalf("undelete = %#v", undel)
	}

	org := store.DefaultOrganizationID
	policyBody := []byte(`{"policy":{"bindings":[{"role":"roles/viewer","members":["user:lab@example.com"]}],"etag":"ACAB"}}`)
	req = httptest.NewRequest(http.MethodPost, "/v3/organizations/"+org+":setIamPolicy", bytes.NewReader(policyBody))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setIamPolicy status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v3/organizations/"+org+":getIamPolicy", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("getIamPolicy status=%d body=%s", rec.Code, rec.Body.String())
	}
	var pol map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &pol)
	bindings, _ := pol["bindings"].([]any)
	if len(bindings) != 1 {
		t.Fatalf("policy = %#v", pol)
	}

	labelsBody := []byte(`{"labels":{"env":"lab"}}`)
	req = httptest.NewRequest(http.MethodPatch, "/v3/projects/noctaxris-gcp-local?updateMask=labels", bytes.NewReader(labelsBody))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch labels status=%d body=%s", rec.Code, rec.Body.String())
	}
	var proj map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &proj)
	labels, _ := proj["labels"].(map[string]any)
	if labels["env"] != "lab" {
		t.Fatalf("labels = %#v", proj["labels"])
	}

	// Seeded org still present.
	req = httptest.NewRequest(http.MethodGet, "/v3/organizations/"+org, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get org status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCRMFailClosedWithoutPrincipal(t *testing.T) {
	mux := http.NewServeMux()
	h := &resourcemanager.Handler{
		Principal: func(*http.Request) (authn.Principal, bool) { return authn.Principal{}, false },
	}
	h.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/v3/projects/noctaxris-gcp-local", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}
}
