package artifactregistry_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/artifactregistry"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestArtifactRegistryDeepenIAMFilesTagsLabels(t *testing.T) {
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
	svc := &artifactregistry.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	})

	loc := artifactregistry.DefaultLocation
	base := "/v1/projects/noctaxris-gcp-local/locations/" + loc + "/repositories"
	req := httptest.NewRequest(http.MethodPost, base+"?repositoryId=lab",
		bytes.NewReader([]byte(`{"format":"DOCKER","description":"d","labels":{"env":"lab"}}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create repo status=%d body=%s", rec.Code, rec.Body.String())
	}

	pol := `{"policy":{"etag":"ACAB","bindings":[{"role":"roles/artifactregistry.reader","members":["allAuthenticatedUsers"]}]}}`
	req = httptest.NewRequest(http.MethodPost, base+"/lab:setIamPolicy", bytes.NewReader([]byte(pol)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setIamPolicy status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base+"/lab:getIamPolicy", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("getIamPolicy status=%d body=%s", rec.Code, rec.Body.String())
	}
	var gotPol map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &gotPol)
	bindings, _ := gotPol["bindings"].([]any)
	if len(bindings) != 1 {
		t.Fatalf("policy=%#v", gotPol)
	}

	req = httptest.NewRequest(http.MethodPatch, base+"/lab?updateMask=labels,description",
		bytes.NewReader([]byte(`{"labels":{"env":"prod","tier":"1"},"description":"updated"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch labels status=%d body=%s", rec.Code, rec.Body.String())
	}
	var repo map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &repo)
	labels, _ := repo["labels"].(map[string]any)
	if labels["env"] != "prod" || labels["tier"] != "1" || repo["description"] != "updated" {
		t.Fatalf("patched repo=%#v", repo)
	}

	req = httptest.NewRequest(http.MethodPost, base+"/lab/packages?packageId=hello",
		bytes.NewReader([]byte(`{"displayName":"hello"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create package status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, base+"/lab/packages/hello/versions?versionId=sha256:abc",
		bytes.NewReader([]byte(`{"relatedTags":["latest","v1"]}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create version status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base+"/lab/files", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list files status=%d body=%s", rec.Code, rec.Body.String())
	}
	var filesBody map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &filesBody)
	files, _ := filesBody["files"].([]any)
	if len(files) != 1 {
		t.Fatalf("files=%#v", filesBody)
	}

	req = httptest.NewRequest(http.MethodGet, base+"/lab/packages/hello/tags", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list tags status=%d body=%s", rec.Code, rec.Body.String())
	}
	var tagsBody map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &tagsBody)
	tags, _ := tagsBody["tags"].([]any)
	if len(tags) != 2 {
		t.Fatalf("tags=%#v", tagsBody)
	}
}

func TestArtifactRegistryRepositoryPackageVersionCRUD(t *testing.T) {
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
	svc := &artifactregistry.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	})
	loc := artifactregistry.DefaultLocation
	base := "/v1/projects/noctaxris-gcp-local/locations/" + loc + "/repositories"

	req := httptest.NewRequest(http.MethodPost, base+"?repositoryId=crud",
		bytes.NewReader([]byte(`{"format":"DOCKER"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create repo status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list repos status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, base+"/crud/packages?packageId=app",
		bytes.NewReader([]byte(`{}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create package status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, base+"/crud/packages/app/versions?versionId=v1",
		bytes.NewReader([]byte(`{}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create version status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, base+"/crud/packages/app/versions/v1", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete version status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, base+"/crud/packages/app", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete package status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, base+"/crud", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete repo status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestArtifactRegistryFailClosed(t *testing.T) {
	mux := http.NewServeMux()
	svc := &artifactregistry.Service{}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) { return authn.Principal{}, false })
	req := httptest.NewRequest(http.MethodGet, "/v1/projects/p/locations/us-central1/repositories", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestArtifactRegistryAuthzDenyNonRootWithoutBinding(t *testing.T) {
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
	svc := &artifactregistry.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/projects/noctaxris-gcp-local/locations/us-central1/repositories", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}
