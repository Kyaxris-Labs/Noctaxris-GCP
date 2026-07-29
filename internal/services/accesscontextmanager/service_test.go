package accesscontextmanager_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/accesscontextmanager"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func acmMux(t *testing.T) (*http.ServeMux, *store.Store) {
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
	root := "root@noctaxris-gcp-local.iam.gserviceaccount.com"
	if err := st.EnsureRoot("noctaxris-gcp-local", root); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	principalFrom := func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: root, IsRoot: true}, true
	}
	(&accesscontextmanager.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}).Mount(mux, principalFrom)
	return mux, st
}

func TestAccessPoliciesAndPerimetersCRUD(t *testing.T) {
	mux, _ := acmMux(t)

	req := httptest.NewRequest(http.MethodPost, "/v1/accessPolicies?policyId=lab1", bytes.NewReader([]byte(
		`{"parent":"organizations/noctaxris-gcp-org","title":"Lab Policy"}`,
	)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create policy status=%d body=%s", rec.Code, rec.Body.String())
	}
	var createOp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &createOp)
	if createOp["done"] != true {
		t.Fatalf("create expected done Operation: %#v", createOp)
	}
	pol, _ := createOp["response"].(map[string]any)
	if pol == nil || pol["name"] != "accessPolicies/lab1" {
		t.Fatalf("policy response=%#v", createOp)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/accessPolicies?parent=organizations/noctaxris-gcp-org", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list policies status=%d body=%s", rec.Code, rec.Body.String())
	}

	body := `{
		"title":"Perimeter A",
		"status":{
			"resources":["projects/noctaxris-gcp-local"],
			"restrictedServices":["storage.googleapis.com","pubsub.googleapis.com"]
		}
	}`
	req = httptest.NewRequest(http.MethodPost,
		"/v1/accessPolicies/lab1/servicePerimeters?servicePerimeterId=p-a",
		bytes.NewReader([]byte(body)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create perimeter status=%d body=%s", rec.Code, rec.Body.String())
	}
	var perimOp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &perimOp)
	if perimOp["done"] != true {
		t.Fatalf("perimeter create expected done: %#v", perimOp)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/accessPolicies/lab1/servicePerimeters/p-a", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get perimeter status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got["name"] != "accessPolicies/lab1/servicePerimeters/p-a" {
		t.Fatalf("get perimeter=%#v", got)
	}
	status, _ := got["status"].(map[string]any)
	if status == nil {
		t.Fatalf("missing status: %#v", got)
	}

	req = httptest.NewRequest(http.MethodPatch,
		"/v1/accessPolicies/lab1/servicePerimeters/p-a?updateMask=title",
		bytes.NewReader([]byte(`{"title":"Perimeter A2"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch perimeter status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/v1/accessPolicies/lab1/servicePerimeters/p-a", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete perimeter status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/v1/accessPolicies/lab1", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete policy status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAccessPolicyAuthzDeny(t *testing.T) {
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
	principalFrom := func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "viewer@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: false}, true
	}
	(&accesscontextmanager.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}).Mount(mux, principalFrom)

	req := httptest.NewRequest(http.MethodPost, "/v1/accessPolicies?policyId=denied",
		bytes.NewReader([]byte(`{"parent":"organizations/noctaxris-gcp-org","title":"x"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUnauthenticated(t *testing.T) {
	mux, _ := acmMux(t)
	// Remount with no principal
	mux2 := http.NewServeMux()
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
	(&accesscontextmanager.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}).Mount(mux2, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{}, false
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/accessPolicies", nil)
	rec := httptest.NewRecorder()
	mux2.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	_ = mux
}
