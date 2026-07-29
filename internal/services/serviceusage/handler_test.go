package serviceusage_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/serviceusage"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func setupServiceUsage(t *testing.T) (*http.ServeMux, string) {
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
	project := "noctaxris-gcp-local"
	rootSA := "root@noctaxris-gcp-local.iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, rootSA); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h := &serviceusage.Handler{
		Store: st,
		Authz: &authz.Evaluator{Policies: st},
		Principal: func(*http.Request) (authn.Principal, bool) {
			return authn.Principal{Email: rootSA, IsRoot: true}, true
		},
	}
	h.Mount(mux)
	return mux, project
}

func TestServiceUsageEnableDisableAndBatch(t *testing.T) {
	mux, project := setupServiceUsage(t)
	base := "/v1/projects/" + project + "/services/"

	disableReq := httptest.NewRequest(http.MethodPost, base+"storage.googleapis.com:disable", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, disableReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable status=%d body=%s", rec.Code, rec.Body.String())
	}
	var op map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &op)
	resp, _ := op["response"].(map[string]any)
	if resp["state"] != "DISABLED" {
		t.Fatalf("disable op = %#v", op)
	}

	getReq := httptest.NewRequest(http.MethodGet, base+"storage.googleapis.com", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, getReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}
	var svc map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &svc)
	if svc["state"] != "DISABLED" {
		t.Fatalf("get after disable = %#v", svc)
	}

	enableReq := httptest.NewRequest(http.MethodPost, base+"storage.googleapis.com:enable", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, enableReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("enable status=%d body=%s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &op)
	resp, _ = op["response"].(map[string]any)
	if resp["state"] != "ENABLED" {
		t.Fatalf("enable op = %#v", op)
	}

	batchDisable := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/services:batchDisable",
		bytes.NewReader([]byte(`{"serviceIds":["pubsub.googleapis.com"]}`)))
	batchDisable.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, batchDisable)
	if rec.Code != http.StatusOK {
		t.Fatalf("batchDisable status=%d body=%s", rec.Code, rec.Body.String())
	}

	filterReq := httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/services?filter=state:DISABLED", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, filterReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("list disabled status=%d body=%s", rec.Code, rec.Body.String())
	}
	var listBody map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &listBody)
	services, _ := listBody["services"].([]any)
	foundPubsub := false
	for _, raw := range services {
		s, _ := raw.(map[string]any)
		if s["state"] != "DISABLED" {
			t.Fatalf("non-disabled in filter: %#v", s)
		}
		name, _ := s["name"].(string)
		if name == "projects/"+project+"/services/pubsub.googleapis.com" {
			foundPubsub = true
		}
	}
	if !foundPubsub {
		t.Fatalf("pubsub not in disabled list: %#v", listBody)
	}

	batchEnable := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/services:batchEnable",
		bytes.NewReader([]byte(`{"serviceIds":["storage.googleapis.com","pubsub.googleapis.com"]}`)))
	batchEnable.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, batchEnable)
	if rec.Code != http.StatusOK {
		t.Fatalf("batchEnable status=%d body=%s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &op)
	if op["done"] != true {
		t.Fatalf("batchEnable = %#v", op)
	}
}

func TestServiceUsageFailClosedWithoutPrincipal(t *testing.T) {
	mux := http.NewServeMux()
	h := &serviceusage.Handler{
		Principal: func(*http.Request) (authn.Principal, bool) { return authn.Principal{}, false },
	}
	h.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/v1/projects/noctaxris-gcp-local/services", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestServiceUsageAuthzDenyNonRoot(t *testing.T) {
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
	h := &serviceusage.Handler{
		Store: st,
		Authz: &authz.Evaluator{Policies: st},
		Principal: func(*http.Request) (authn.Principal, bool) {
			return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
		},
	}
	h.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/services", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}
