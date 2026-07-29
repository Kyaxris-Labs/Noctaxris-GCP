package gke_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/gke"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestGKEClustersCRUD(t *testing.T) {
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
	svc := &gke.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	})

	loc := gke.DefaultLocation
	base := "/container/v1/projects/noctaxris-gcp-local/locations/" + loc + "/clusters"
	req := httptest.NewRequest(http.MethodPost, base+"?clusterId=lab-gke", bytes.NewReader([]byte(`{"displayName":"Lab GKE"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var cluster map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &cluster)
	if cluster["status"] != "RUNNING" {
		t.Fatalf("cluster=%#v", cluster)
	}
	if cluster["endpoint"] == "" {
		t.Fatal("expected endpoint")
	}

	req = httptest.NewRequest(http.MethodGet, base+"/lab-gke", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, base, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodDelete, base+"/lab-gke", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d", rec.Code)
	}
}

func TestGKEAuthzFailClosed(t *testing.T) {
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
	svc := &gke.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})

	req := httptest.NewRequest(http.MethodGet, "/container/v1/projects/noctaxris-gcp-local/locations/us-central1/clusters", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestGKENilAuthzFailClosed(t *testing.T) {
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

	mux := http.NewServeMux()
	svc := &gke.Service{Store: st, Authz: nil}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})

	req := httptest.NewRequest(http.MethodGet, "/container/v1/projects/noctaxris-gcp-local/locations/us-central1/clusters", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("nil Authz expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}
