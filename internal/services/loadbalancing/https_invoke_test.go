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

func TestLoadBalancingInvokeViaHTTPSProxy(t *testing.T) {
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
	if _, _, err := st.CreateBucket("proxy-bucket", project, "US", "STANDARD"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutObjectBytes("proxy-bucket", "p/hi.txt", "text/plain", []byte("via-proxy")); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	lb := &loadbalancing.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	lb.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@" + project + ".iam.gserviceaccount.com", IsRoot: true}, true
	})

	bsBody := `{"name":"proxy-bs","backends":[{"gcsBucket":"proxy-bucket","objectPrefix":"p"}],"description":"d","securityPolicy":"projects/` + project + `/global/securityPolicies/sp1"}`
	req := httptest.NewRequest(http.MethodPost, "/compute/v1/projects/"+project+"/global/backendServices", bytes.NewReader([]byte(bsBody)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("backend: %d %s", rec.Code, rec.Body.String())
	}
	bsLink := "projects/" + project + "/global/backendServices/proxy-bs"
	mapPayload, _ := json.Marshal(map[string]any{"name": "proxy-map", "defaultService": bsLink, "description": "m"})
	req = httptest.NewRequest(http.MethodPost, "/compute/v1/projects/"+project+"/global/urlMaps", bytes.NewReader(mapPayload))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("urlmap: %d %s", rec.Code, rec.Body.String())
	}
	mapLink := "projects/" + project + "/global/urlMaps/proxy-map"
	proxyBody, _ := json.Marshal(map[string]any{
		"name": "proxy-https", "urlMap": mapLink, "description": "p",
		"sslCertificates": []string{"projects/" + project + "/global/sslCertificates/c1"},
	})
	req = httptest.NewRequest(http.MethodPost, "/compute/v1/projects/"+project+"/global/targetHttpsProxies", bytes.NewReader(proxyBody))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("proxy: %d %s", rec.Code, rec.Body.String())
	}
	proxyLink := "projects/" + project + "/global/targetHttpsProxies/proxy-https"
	frPayload, _ := json.Marshal(map[string]any{"name": "proxy-fr", "target": proxyLink, "description": "fr"})
	req = httptest.NewRequest(http.MethodPost, "/compute/v1/projects/"+project+"/global/forwardingRules", bytes.NewReader(frPayload))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("fr: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/lb/"+project+"/proxy-fr/hi.txt", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("invoke via proxy: %d %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "via-proxy" {
		t.Fatalf("body=%q", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/compute/v1/projects/"+project+"/global/backendServices/proxy-bs", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get backend: %d", rec.Code)
	}
}
