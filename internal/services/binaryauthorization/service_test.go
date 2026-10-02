package binaryauthorization_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/binaryauthorization"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func mountBinaryAuthz(t *testing.T, principal func(*http.Request) (authn.Principal, bool)) (*http.ServeMux, string) {
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
	const project = "noctaxris-gcp-local"
	root := "root@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, root); err != nil {
		t.Fatal(err)
	}
	if principal == nil {
		principal = func(*http.Request) (authn.Principal, bool) {
			return authn.Principal{Email: root, IsRoot: true}, true
		}
	}
	mux := http.NewServeMux()
	(&binaryauthorization.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}).Mount(mux, principal)
	return mux, project
}

func TestBinaryAuthzDefaultPolicyThenUpdate(t *testing.T) {
	mux, project := mountBinaryAuthz(t, nil)
	path := "/v1/projects/" + project + "/policy"

	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("default get status=%d body=%s", rec.Code, rec.Body.String())
	}
	var def map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &def); err != nil {
		t.Fatal(err)
	}
	rule, _ := def["defaultAdmissionRule"].(map[string]any)
	if rule["evaluationMode"] != "ALWAYS_ALLOW" {
		t.Fatalf("default policy=%#v", def)
	}

	body := `{
		"defaultAdmissionRule": {
			"evaluationMode": "REQUIRE_ATTESTATION",
			"enforcementMode": "DRYRUN_AUDIT_LOG_ONLY"
		},
		"description": "lab policy"
	}`
	req = httptest.NewRequest(http.MethodPut, path, bytes.NewReader([]byte(body)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", rec.Code, rec.Body.String())
	}
	var updated map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated["name"] != "projects/"+project+"/policy" {
		t.Fatalf("name=%v", updated["name"])
	}
	rule, _ = updated["defaultAdmissionRule"].(map[string]any)
	if rule["enforcementMode"] != "DRYRUN_AUDIT_LOG_ONLY" {
		t.Fatalf("updated rule=%#v", rule)
	}

	req = httptest.NewRequest(http.MethodGet, path, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get after update status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	rule, _ = got["defaultAdmissionRule"].(map[string]any)
	if rule["enforcementMode"] != "DRYRUN_AUDIT_LOG_ONLY" {
		t.Fatalf("persisted rule=%#v", rule)
	}
}

func TestBinaryAuthzEnforcementModeFromTopLevel(t *testing.T) {
	mux, project := mountBinaryAuthz(t, nil)
	path := "/v1/projects/" + project + "/policy"
	req := httptest.NewRequest(http.MethodPut, path, bytes.NewReader([]byte(`{"enforcementMode":"ENFORCED_BLOCK_AND_AUDIT_LOG"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	rule, _ := got["defaultAdmissionRule"].(map[string]any)
	if rule["enforcementMode"] != "ENFORCED_BLOCK_AND_AUDIT_LOG" {
		t.Fatalf("rule=%#v", rule)
	}
}

func TestBinaryAuthzEmptyBodyDefaultsEnforced(t *testing.T) {
	mux, project := mountBinaryAuthz(t, nil)
	path := "/v1/projects/" + project + "/policy"
	req := httptest.NewRequest(http.MethodPut, path, bytes.NewReader([]byte(`{}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	rule, _ := got["defaultAdmissionRule"].(map[string]any)
	if rule["enforcementMode"] != "ENFORCED_BLOCK_AND_AUDIT_LOG" {
		t.Fatalf("rule=%#v", rule)
	}
}

func TestBinaryAuthzUnauthenticatedAndDenied(t *testing.T) {
	mux, project := mountBinaryAuthz(t, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{}, false
	})
	path := "/v1/projects/" + project + "/policy"
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth status=%d body=%s", rec.Code, rec.Body.String())
	}

	mux, project = mountBinaryAuthz(t, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})
	req = httptest.NewRequest(http.MethodGet, path, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("deny get status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPut, path, bytes.NewReader([]byte(`{}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("deny put status=%d body=%s", rec.Code, rec.Body.String())
	}
}
