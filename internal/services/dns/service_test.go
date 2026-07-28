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

func TestDNSChangesCreateGetList(t *testing.T) {
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
	req := httptest.NewRequest(http.MethodPost, base, bytes.NewReader([]byte(
		`{"name":"chg-zone","dnsName":"chg.example.com.","visibility":"public"}`,
	)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create zone status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Create A via Changes.create (Terraform google_dns_record_set path).
	changeBody := `{
		"additions":[{"name":"www.chg.example.com.","type":"A","ttl":120,"rrdatas":["9.9.9.9"]}],
		"deletions":[]
	}`
	req = httptest.NewRequest(http.MethodPost, base+"/chg-zone/changes", bytes.NewReader([]byte(changeBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create change status=%d body=%s", rec.Code, rec.Body.String())
	}
	var change map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &change); err != nil {
		t.Fatal(err)
	}
	if change["status"] != "done" || change["kind"] != "dns#change" {
		t.Fatalf("change=%#v", change)
	}
	changeID, _ := change["id"].(string)
	if changeID == "" {
		t.Fatalf("missing change id: %#v", change)
	}

	req = httptest.NewRequest(http.MethodGet, base+"/chg-zone/rrsets/www.chg.example.com./A", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get rrset after change status=%d body=%s", rec.Code, rec.Body.String())
	}
	var rr map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &rr)
	rrdatas, _ := rr["rrdatas"].([]any)
	if len(rrdatas) != 1 || rrdatas[0] != "9.9.9.9" {
		t.Fatalf("rrset after add=%#v", rr)
	}

	req = httptest.NewRequest(http.MethodGet, base+"/chg-zone/changes/"+changeID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get change status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got["status"] != "done" || got["id"] != changeID {
		t.Fatalf("get change=%#v", got)
	}

	// Update via deletion + addition.
	updBody := `{
		"deletions":[{"name":"www.chg.example.com.","type":"A","ttl":120,"rrdatas":["9.9.9.9"]}],
		"additions":[{"name":"www.chg.example.com.","type":"A","ttl":300,"rrdatas":["1.1.1.1"]}]
	}`
	req = httptest.NewRequest(http.MethodPost, base+"/chg-zone/changes", bytes.NewReader([]byte(updBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update change status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, base+"/chg-zone/rrsets/www.chg.example.com./A", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	_ = json.Unmarshal(rec.Body.Bytes(), &rr)
	rrdatas, _ = rr["rrdatas"].([]any)
	if len(rrdatas) != 1 || rrdatas[0] != "1.1.1.1" {
		t.Fatalf("rrset after update=%#v", rr)
	}

	// Delete via Changes.create deletions only.
	delBody := `{
		"deletions":[{"name":"www.chg.example.com.","type":"A","ttl":300,"rrdatas":["1.1.1.1"]}],
		"additions":[]
	}`
	req = httptest.NewRequest(http.MethodPost, base+"/chg-zone/changes", bytes.NewReader([]byte(delBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete change status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, base+"/chg-zone/rrsets/www.chg.example.com./A", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected rrset gone, status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base+"/chg-zone/changes", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list changes status=%d body=%s", rec.Code, rec.Body.String())
	}
	var listBody map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &listBody)
	changes, _ := listBody["changes"].([]any)
	if len(changes) < 3 {
		t.Fatalf("expected >=3 changes, got %#v", listBody)
	}
}
