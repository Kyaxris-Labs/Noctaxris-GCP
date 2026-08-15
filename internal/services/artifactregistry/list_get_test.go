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

func TestArtifactRegistryListGetPackageVersion(t *testing.T) {
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

	req := httptest.NewRequest(http.MethodPost, base+"?repositoryId=lg",
		bytes.NewReader([]byte(`{"format":"DOCKER"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create repo: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, base+"/lg", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get repo: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, base+"/lg/packages?packageId=pkg",
		bytes.NewReader([]byte(`{"displayName":"pkg"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create pkg: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base+"/lg/packages", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list packages: %d %s", rec.Code, rec.Body.String())
	}
	var pkgs map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &pkgs)
	if len(pkgs["packages"].([]any)) < 1 {
		t.Fatalf("packages=%#v", pkgs)
	}

	req = httptest.NewRequest(http.MethodGet, base+"/lg/packages/pkg", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get package: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, base+"/lg/packages/pkg/versions?versionId=1.2.3",
		bytes.NewReader([]byte(`{"description":"v"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create version: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base+"/lg/packages/pkg/versions", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list versions: %d %s", rec.Code, rec.Body.String())
	}
	var vers map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &vers)
	if len(vers["versions"].([]any)) < 1 {
		t.Fatalf("versions=%#v", vers)
	}

	req = httptest.NewRequest(http.MethodGet, base+"/lg/packages/pkg/versions/1.2.3", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get version: %d %s", rec.Code, rec.Body.String())
	}
}
