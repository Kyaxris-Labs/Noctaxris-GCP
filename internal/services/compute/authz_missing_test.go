package compute_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/compute"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestComputeAuthzDenyAndMissing(t *testing.T) {
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
	if err := st.EnsureRoot(project, "root@"+project+".iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	(&compute.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}).Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})
	paths := []struct {
		method, path string
		body         string
	}{
		{http.MethodGet, "/compute/v1/projects/" + project + "/global/networks", ""},
		{http.MethodPost, "/compute/v1/projects/" + project + "/global/networks", `{"name":"x"}`},
		{http.MethodGet, "/compute/v1/projects/" + project + "/global/firewalls", ""},
		{http.MethodGet, "/compute/v1/projects/" + project + "/zones/us-central1-a/instances", ""},
		{http.MethodGet, "/compute/v1/projects/" + project + "/global/images", ""},
		{http.MethodGet, "/compute/v1/projects/" + project + "/regions", ""},
	}
	for _, p := range paths {
		var req *http.Request
		if p.body == "" {
			req = httptest.NewRequest(p.method, p.path, nil)
		} else {
			req = httptest.NewRequest(p.method, p.path, bytes.NewReader([]byte(p.body)))
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s want 403 got %d %s", p.method, p.path, rec.Code, rec.Body.String())
		}
	}

	mux2, _, project := mountCompute(t)
	req := httptest.NewRequest(http.MethodGet, "/compute/v1/projects/"+project+"/global/networks/missing", nil)
	rec := httptest.NewRecorder()
	mux2.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing net: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodDelete, "/compute/v1/projects/"+project+"/global/networks/missing", nil)
	rec = httptest.NewRecorder()
	mux2.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing net: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/compute/v1/projects/"+project+"/regions/us-central1/subnetworks/missing", nil)
	rec = httptest.NewRecorder()
	mux2.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing subnet: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/compute/v1/projects/"+project+"/global/firewalls/missing", nil)
	rec = httptest.NewRecorder()
	mux2.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing fw: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/compute/v1/projects/"+project+"/zones/us-central1-a/instances/missing", nil)
	rec = httptest.NewRecorder()
	mux2.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing vm: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "/compute/v1/projects/"+project+"/global/networks", bytes.NewReader([]byte(`not-json`)))
	rec = httptest.NewRecorder()
	mux2.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("bad json net")
	}
}
