package dns_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/dns"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestDNSZonesAndRrsetsCRUD(t *testing.T) {
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
	svc := &dns.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	})

	base := "/dns/v1/projects/noctaxris-gcp-local/managedZones"
	body := `{"name":"example-zone","dnsName":"example.com.","description":"lab","visibility":"public"}`
	req := httptest.NewRequest(http.MethodPost, base, bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create zone status=%d body=%s", rec.Code, rec.Body.String())
	}
	var zone map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &zone); err != nil {
		t.Fatal(err)
	}
	if zone["name"] != "example-zone" || zone["dnsName"] != "example.com." || zone["visibility"] != "public" {
		t.Fatalf("zone=%#v", zone)
	}

	req = httptest.NewRequest(http.MethodGet, base+"/example-zone", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get zone status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list zones status=%d body=%s", rec.Code, rec.Body.String())
	}

	rrBody := `{"name":"www.example.com.","type":"A","ttl":300,"rrdatas":["1.2.3.4"]}`
	req = httptest.NewRequest(http.MethodPost, base+"/example-zone/rrsets", bytes.NewReader([]byte(rrBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create rrset status=%d body=%s", rec.Code, rec.Body.String())
	}
	var rr map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &rr)
	if rr["type"] != "A" {
		t.Fatalf("rrset=%#v", rr)
	}

	req = httptest.NewRequest(http.MethodGet, base+"/example-zone/rrsets", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list rrsets status=%d body=%s", rec.Code, rec.Body.String())
	}
	var listBody map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &listBody)
	rrsets, _ := listBody["rrsets"].([]any)
	// NS + SOA seeded + A record
	if len(rrsets) < 3 {
		t.Fatalf("expected seeded NS/SOA plus A: %#v", listBody)
	}

	req = httptest.NewRequest(http.MethodGet, base+"/example-zone/rrsets/www.example.com./A", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get rrset status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, base+"/example-zone/rrsets/www.example.com./A", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete rrset status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, base+"/example-zone", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete zone status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDNSFailClosedWithoutPrincipal(t *testing.T) {
	mux := http.NewServeMux()
	svc := &dns.Service{}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) { return authn.Principal{}, false })
	req := httptest.NewRequest(http.MethodGet, "/dns/v1/projects/p/managedZones", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}
}
