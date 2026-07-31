package loadbalancing_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/loadbalancing"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestLoadBalancingInvokeGCS(t *testing.T) {
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
	if _, _, err := st.CreateBucket("cdn-bucket", "noctaxris-gcp-local", "US", "STANDARD"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutObjectBytes("cdn-bucket", "static/hello.txt", "text/plain", []byte("lb-ok")); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	principal := func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	}
	lb := &loadbalancing.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	lb.Mount(mux, principal)

	project := "noctaxris-gcp-local"
	bsBody := `{"name":"lab-bs","backends":[{"gcsBucket":"cdn-bucket","objectPrefix":"static"}]}`
	req := httptest.NewRequest(http.MethodPost, "/compute/v1/projects/"+project+"/global/backendServices", bytes.NewReader([]byte(bsBody)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("backend status=%d body=%s", rec.Code, rec.Body.String())
	}
	var createOp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &createOp)
	if createOp["status"] != "DONE" {
		t.Fatalf("backend create expected DONE operation, got %#v", createOp)
	}
	selfLink := "projects/" + project + "/global/backendServices/lab-bs"

	mapPayload, _ := json.Marshal(map[string]any{"name": "lab-map", "defaultService": selfLink})
	req = httptest.NewRequest(http.MethodPost, "/compute/v1/projects/"+project+"/global/urlMaps", bytes.NewReader(mapPayload))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("url map status=%d body=%s", rec.Code, rec.Body.String())
	}
	var um map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &um)
	mapLink, _ := um["selfLink"].(string)

	frPayload, _ := json.Marshal(map[string]any{"name": "lab-fr", "target": mapLink})
	req = httptest.NewRequest(http.MethodPost, "/compute/v1/projects/"+project+"/global/forwardingRules", bytes.NewReader(frPayload))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("forwarding rule status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/lb/"+project+"/lab-fr/hello.txt", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("invoke status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "lb-ok" {
		t.Fatalf("body=%q", rec.Body.String())
	}
}

func TestLoadBalancingAuthzFailClosed(t *testing.T) {
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
	lb := &loadbalancing.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	lb.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})

	req := httptest.NewRequest(http.MethodGet, "/compute/v1/projects/noctaxris-gcp-local/global/backendServices", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLoadBalancingNilAuthzFailClosed(t *testing.T) {
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
	lb := &loadbalancing.Service{Store: st, Authz: nil}
	lb.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})

	req := httptest.NewRequest(http.MethodGet, "/compute/v1/projects/noctaxris-gcp-local/global/backendServices", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("nil Authz expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestBackendServiceSecurityPolicyRoundTrip(t *testing.T) {
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
	principal := func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	}
	lb := &loadbalancing.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	lb.Mount(mux, principal)

	project := "noctaxris-gcp-local"
	policy := "projects/" + project + "/global/securityPolicies/lab-armor"
	policyURL := "https://www.googleapis.com/compute/v1/" + policy
	bsBody, _ := json.Marshal(map[string]any{
		"name":           "armor-bs",
		"securityPolicy": policy,
	})
	req := httptest.NewRequest(http.MethodPost, "/compute/v1/projects/"+project+"/global/backendServices", bytes.NewReader(bsBody))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create backend status=%d body=%s", rec.Code, rec.Body.String())
	}
	var createOp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &createOp); err != nil {
		t.Fatal(err)
	}
	if createOp["status"] != "DONE" || createOp["kind"] != "compute#operation" {
		t.Fatalf("create expected DONE operation, got %#v", createOp)
	}

	req = httptest.NewRequest(http.MethodGet, "/compute/v1/projects/"+project+"/global/backendServices/armor-bs", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get backend status=%d body=%s", rec.Code, rec.Body.String())
	}
	var fetched map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &fetched); err != nil {
		t.Fatal(err)
	}
	if got, _ := fetched["securityPolicy"].(string); got != policyURL {
		t.Fatalf("get securityPolicy=%q want %q", got, policyURL)
	}

	policy2 := policy + "-v2"
	policy2URL := "https://www.googleapis.com/compute/v1/" + policy2
	setBody, _ := json.Marshal(map[string]any{"securityPolicy": policy2})
	req = httptest.NewRequest(http.MethodPost, "/compute/v1/projects/"+project+"/global/backendServices/armor-bs/setSecurityPolicy", bytes.NewReader(setBody))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setSecurityPolicy status=%d body=%s", rec.Code, rec.Body.String())
	}
	var setOp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &setOp); err != nil {
		t.Fatal(err)
	}
	if setOp["status"] != "DONE" {
		t.Fatalf("setSecurityPolicy op=%#v", setOp)
	}

	req = httptest.NewRequest(http.MethodGet, "/compute/v1/projects/"+project+"/global/backendServices/armor-bs", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var afterSet map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &afterSet)
	if got, _ := afterSet["securityPolicy"].(string); got != policy2URL {
		t.Fatalf("after setSecurityPolicy=%q want %q", got, policy2URL)
	}

	policy3 := policy + "-v3"
	policy3URL := "https://www.googleapis.com/compute/v1/" + policy3
	patchBody, _ := json.Marshal(map[string]any{"securityPolicy": policy3})
	req = httptest.NewRequest(http.MethodPatch, "/compute/v1/projects/"+project+"/global/backendServices/armor-bs", bytes.NewReader(patchBody))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch backend status=%d body=%s", rec.Code, rec.Body.String())
	}
	var patched map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &patched); err != nil {
		t.Fatal(err)
	}
	if got, _ := patched["securityPolicy"].(string); got != policy3URL {
		t.Fatalf("patch securityPolicy=%q want %q", got, policy3URL)
	}

	delReq := httptest.NewRequest(http.MethodDelete, "/compute/v1/projects/"+project+"/global/backendServices/armor-bs", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, delReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	var delOp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &delOp)
	if delOp["status"] != "DONE" || delOp["kind"] != "compute#operation" {
		t.Fatalf("delete expected DONE operation, got %#v", delOp)
	}
}

func TestTargetHttpsProxyCRUD(t *testing.T) {
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
	principal := func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	}
	lb := &loadbalancing.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	lb.Mount(mux, principal)

	project := "noctaxris-gcp-local"
	urlMap := "projects/" + project + "/global/urlMaps/lab-map"
	policy := "projects/" + project + "/global/securityPolicies/edge-armor"
	createBody, _ := json.Marshal(map[string]any{
		"name":           "lab-https-proxy",
		"urlMap":           urlMap,
		"securityPolicy": policy,
	})
	req := httptest.NewRequest(http.MethodPost, "/compute/v1/projects/"+project+"/global/targetHttpsProxies", bytes.NewReader(createBody))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create proxy status=%d body=%s", rec.Code, rec.Body.String())
	}
	var createOp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &createOp); err != nil {
		t.Fatal(err)
	}
	if createOp["status"] != "DONE" || createOp["kind"] != "compute#operation" {
		t.Fatalf("create expected DONE operation, got %#v", createOp)
	}

	req = httptest.NewRequest(http.MethodGet, "/compute/v1/projects/"+project+"/global/targetHttpsProxies/lab-https-proxy", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get proxy status=%d body=%s", rec.Code, rec.Body.String())
	}
	var gotProxy map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &gotProxy)
	if got, _ := gotProxy["urlMap"].(string); got != urlMap {
		t.Fatalf("get urlMap=%q want %q", got, urlMap)
	}
	if got, _ := gotProxy["securityPolicy"].(string); got != policy {
		t.Fatalf("get securityPolicy=%q want %q", got, policy)
	}

	req = httptest.NewRequest(http.MethodGet, "/compute/v1/projects/"+project+"/global/targetHttpsProxies", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list proxy status=%d body=%s", rec.Code, rec.Body.String())
	}
	var listed map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	items, _ := listed["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("list items=%d want 1", len(items))
	}

	policy2 := policy + "-patched"
	patchBody, _ := json.Marshal(map[string]any{"securityPolicy": policy2})
	req = httptest.NewRequest(http.MethodPatch, "/compute/v1/projects/"+project+"/global/targetHttpsProxies/lab-https-proxy", bytes.NewReader(patchBody))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch proxy status=%d body=%s", rec.Code, rec.Body.String())
	}
	var patched map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &patched); err != nil {
		t.Fatal(err)
	}
	if got, _ := patched["securityPolicy"].(string); got != policy2 {
		t.Fatalf("patch securityPolicy=%q want %q", got, policy2)
	}

	req = httptest.NewRequest(http.MethodDelete, "/compute/v1/projects/"+project+"/global/targetHttpsProxies/lab-https-proxy", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete proxy status=%d body=%s", rec.Code, rec.Body.String())
	}
	var delOp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &delOp)
	if delOp["status"] != "DONE" {
		t.Fatalf("delete expected DONE operation, got %#v", delOp)
	}
	req = httptest.NewRequest(http.MethodGet, "/compute/v1/projects/"+project+"/global/targetHttpsProxies/lab-https-proxy", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get after delete status=%d want 404", rec.Code)
	}
}
