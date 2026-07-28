package filestore_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/filestore"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestFilestoreInstancesCRUD(t *testing.T) {
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
	svc := &filestore.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	})

	loc := filestore.DefaultLocation
	base := "/file/v1/projects/noctaxris-gcp-local/locations/" + loc + "/instances"
	body := `{"tier":"BASIC_HDD","description":"Lab NFS","fileShares":[{"name":"share1","capacityGb":"1024"}],"networks":[{"network":"default","modes":["MODE_IPV4"]}]}`
	req := httptest.NewRequest(http.MethodPost, base+"?instanceId=lab-nfs", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var inst map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &inst)
	if inst["state"] != "READY" || inst["tier"] != "BASIC_HDD" {
		t.Fatalf("instance=%#v", inst)
	}
	shares, _ := inst["fileShares"].([]any)
	if len(shares) != 1 {
		t.Fatalf("fileShares=%#v", inst["fileShares"])
	}

	req = httptest.NewRequest(http.MethodGet, base+"/lab-nfs", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var list map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	items, _ := list["instances"].([]any)
	if len(items) != 1 {
		t.Fatalf("list=%#v", list)
	}

	req = httptest.NewRequest(http.MethodDelete, base+"/lab-nfs", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestFilestoreAuthzFailClosed(t *testing.T) {
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
	svc := &filestore.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})

	req := httptest.NewRequest(http.MethodGet, "/file/v1/projects/noctaxris-gcp-local/locations/us-central1-a/instances", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestFilestoreDoesNotCollideWithMemorystorePath(t *testing.T) {
	mux := http.NewServeMux()
	// Registering both patterns must succeed (distinct prefixes).
	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/instances", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("redis"))
	})
	mux.HandleFunc("GET /file/v1/projects/{project}/locations/{location}/instances", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("file"))
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/projects/p/locations/us-central1/instances", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Body.String() != "redis" {
		t.Fatalf("memorystore path got %q", rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/file/v1/projects/p/locations/us-central1-a/instances", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Body.String() != "file" {
		t.Fatalf("filestore path got %q", rec.Body.String())
	}
}
