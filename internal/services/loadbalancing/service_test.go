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
	var bs map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &bs)
	selfLink, _ := bs["selfLink"].(string)

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
