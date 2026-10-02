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

func TestLoadBalancingGetDeleteMissingAndHTTPSProxy(t *testing.T) {
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
	lb := &loadbalancing.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	lb.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@" + project + ".iam.gserviceaccount.com", IsRoot: true}, true
	})
	base := "/compute/v1/projects/" + project + "/global"

	req := httptest.NewRequest(http.MethodGet, base+"/backendServices/missing", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing backend get status=%d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodDelete, base+"/backendServices/missing", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing backend delete status=%d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, base+"/urlMaps/missing", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing urlmap get status=%d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodDelete, base+"/urlMaps/missing", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing urlmap delete status=%d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, base+"/forwardingRules/missing", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing fr get status=%d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodDelete, base+"/forwardingRules/missing", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing fr delete status=%d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPost, base+"/backendServices/missing/setSecurityPolicy",
		bytes.NewReader([]byte(`{"securityPolicy":"projects/`+project+`/global/securityPolicies/p"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("setSecurityPolicy missing status=%d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPost, base+"/backendServices/missing/setSecurityPolicy",
		bytes.NewReader([]byte(`not-json`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("setSecurityPolicy bad json")
	}

	bsBody := `{"name":"crud-bs","backends":[{"gcsBucket":"b","objectPrefix":"p"}]}`
	req = httptest.NewRequest(http.MethodPost, base+"/backendServices", bytes.NewReader([]byte(bsBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create backend status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, base+"/backendServices/crud-bs", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get backend status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPatch, base+"/backendServices/crud-bs",
		bytes.NewReader([]byte(`{"securityPolicy":"projects/`+project+`/global/securityPolicies/armor"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch backend status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, base+"/backendServices/crud-bs/setSecurityPolicy",
		bytes.NewReader([]byte(`{"securityPolicy":"projects/`+project+`/global/securityPolicies/armor2"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setSecurityPolicy status=%d body=%s", rec.Code, rec.Body.String())
	}

	selfLink := "projects/" + project + "/global/backendServices/crud-bs"
	mapPayload, _ := json.Marshal(map[string]any{"name": "crud-um", "defaultService": selfLink})
	req = httptest.NewRequest(http.MethodPost, base+"/urlMaps", bytes.NewReader(mapPayload))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("urlmap status=%d body=%s", rec.Code, rec.Body.String())
	}
	mapLink := "projects/" + project + "/global/urlMaps/crud-um"

	proxyBody, _ := json.Marshal(map[string]any{"name": "crud-proxy", "urlMap": mapLink})
	req = httptest.NewRequest(http.MethodPost, base+"/targetHttpsProxies", bytes.NewReader(proxyBody))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("https proxy status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, base+"/targetHttpsProxies", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list proxies status=%d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, base+"/targetHttpsProxies/crud-proxy", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get proxy status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPatch, base+"/targetHttpsProxies/crud-proxy",
		bytes.NewReader([]byte(`{"description":"patched"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch proxy status=%d body=%s", rec.Code, rec.Body.String())
	}

	proxyLink := "projects/" + project + "/global/targetHttpsProxies/crud-proxy"
	frPayload, _ := json.Marshal(map[string]any{"name": "crud-fr", "target": proxyLink})
	req = httptest.NewRequest(http.MethodPost, base+"/forwardingRules", bytes.NewReader(frPayload))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("fr status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, base+"/forwardingRules/crud-fr", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get fr status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, base+"/forwardingRules", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list fr status=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodDelete, base+"/forwardingRules/crud-fr", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete fr status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodDelete, base+"/targetHttpsProxies/crud-proxy", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete proxy status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodDelete, base+"/urlMaps/crud-um", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete urlmap status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodDelete, base+"/backendServices/crud-bs", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete backend status=%d body=%s", rec.Code, rec.Body.String())
	}
}
