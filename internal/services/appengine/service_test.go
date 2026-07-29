package appengine_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/appengine"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestAppEngineTrafficSplitMigrateAndInstances(t *testing.T) {
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
	svc := &appengine.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	})

	appID := "noctaxris-gcp-local"
	req := httptest.NewRequest(http.MethodPost, "/v1/apps", bytes.NewReader([]byte(`{"id":"`+appID+`","locationId":"us-central"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create app status=%d body=%s", rec.Code, rec.Body.String())
	}

	verBody := `{"id":"v1","runtime":"python311"}`
	req = httptest.NewRequest(http.MethodPost, "/v1/apps/"+appID+"/services/default/versions", bytes.NewReader([]byte(verBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create version status=%d body=%s", rec.Code, rec.Body.String())
	}

	patch := `{"split":{"allocations":{"v1":1}},"shardBy":"IP"}`
	req = httptest.NewRequest(http.MethodPatch, "/v1/apps/"+appID+"/services/default?migrateTraffic=true", bytes.NewReader([]byte(patch)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch service status=%d body=%s", rec.Code, rec.Body.String())
	}
	var svcBody map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &svcBody)
	if svcBody["shardBy"] != "IP" {
		t.Fatalf("service = %#v", svcBody)
	}
	split, _ := svcBody["split"].(map[string]any)
	alloc, _ := split["allocations"].(map[string]any)
	if alloc["v1"] != float64(1) {
		t.Fatalf("allocations = %#v", alloc)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/apps/"+appID+"/services/default/versions/v1/instances", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list instances status=%d body=%s", rec.Code, rec.Body.String())
	}
	var inst map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &inst)
	list, _ := inst["instances"].([]any)
	if list == nil || len(list) != 0 {
		t.Fatalf("instances = %#v", inst)
	}
}

func TestAppEngineAuthzDenyNonRoot(t *testing.T) {
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
	svc := &appengine.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/apps/noctaxris-gcp-local", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAppEngineFailClosedWithoutPrincipal(t *testing.T) {
	mux := http.NewServeMux()
	svc := &appengine.Service{}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) { return authn.Principal{}, false })
	req := httptest.NewRequest(http.MethodGet, "/v1/apps/x", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}
}
