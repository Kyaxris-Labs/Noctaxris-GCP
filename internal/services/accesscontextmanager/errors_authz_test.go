package accesscontextmanager_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/accesscontextmanager"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestACMCreateErrorsAndAuthz(t *testing.T) {
	mux, _ := acmMux(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/accessPolicies?policyId=bad", bytes.NewReader([]byte(`not-json`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("bad json")
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/accessPolicies/missing", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing policy: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodDelete, "/v1/accessPolicies/missing", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/accessPolicies/p/servicePerimeters/missing", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing perimeter: %d", rec.Code)
	}

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
	deny := http.NewServeMux()
	svc := &accesscontextmanager.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(deny, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})
	req = httptest.NewRequest(http.MethodGet, "/v1/accessPolicies", nil)
	rec = httptest.NewRecorder()
	deny.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("authz: %d %s", rec.Code, rec.Body.String())
	}
}
