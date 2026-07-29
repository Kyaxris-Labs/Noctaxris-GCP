package cloudarmor_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudarmor"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func armorMux(t *testing.T) (*http.ServeMux, string) {
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
	(&cloudarmor.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}).Mount(mux, principalFrom)
	return mux, "noctaxris-gcp-local"
}

func TestSecurityPoliciesCRUDAndByteMatchValidate(t *testing.T) {
	mux, project := armorMux(t)
	base := "/compute/v1/projects/" + project + "/global/securityPolicies"

	req := httptest.NewRequest(http.MethodPost, base, bytes.NewReader([]byte(
		`{"name":"lab-armor","description":"deny admin path"}`,
	)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("insert status=%d body=%s", rec.Code, rec.Body.String())
	}

	rule := `{
		"priority": 1000,
		"action": "deny(403)",
		"preview": false,
		"match": {
			"byteMatchSet": {
				"fieldToMatch": "UriPath",
				"positionalConstraint": "CONTAINS",
				"searchString": "/admin"
			}
		}
	}`
	req = httptest.NewRequest(http.MethodPost, base+"/lab-armor/addRule", bytes.NewReader([]byte(rule)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("addRule status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base+"/lab-armor", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}
	var pol map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &pol)
	rules, _ := pol["rules"].([]any)
	if len(rules) < 2 {
		t.Fatalf("expected default + deny rule: %#v", pol)
	}

	req = httptest.NewRequest(http.MethodPost, base+"/lab-armor:validate",
		bytes.NewReader([]byte(`{"uriPath":"/admin/users","srcIp":"203.0.113.10"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("validate deny status=%d body=%s", rec.Code, rec.Body.String())
	}
	var result map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &result)
	if result["allowed"] != false || result["matched"] != true {
		t.Fatalf("expected deny match: %#v", result)
	}

	req = httptest.NewRequest(http.MethodPost, base+"/lab-armor:validate",
		bytes.NewReader([]byte(`{"uriPath":"/public","srcIp":"203.0.113.10"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	_ = json.Unmarshal(rec.Body.Bytes(), &result)
	if result["allowed"] != true {
		t.Fatalf("expected default allow: %#v", result)
	}

	req = httptest.NewRequest(http.MethodPost, base+"/lab-armor/removeRule?priority=1000", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("removeRule status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, base+"/lab-armor", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSecurityPoliciesAuthzUnauthenticated(t *testing.T) {
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
	principalFrom := func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{}, false
	}
	(&cloudarmor.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}).Mount(mux, principalFrom)
	req := httptest.NewRequest(http.MethodGet, "/compute/v1/projects/p/global/securityPolicies", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestSecurityPoliciesAuthzFailClosed(t *testing.T) {
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
	(&cloudarmor.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}).Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})
	req := httptest.NewRequest(http.MethodGet, "/compute/v1/projects/noctaxris-gcp-local/global/securityPolicies", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}
