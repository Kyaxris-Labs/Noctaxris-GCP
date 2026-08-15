package dns_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/dns"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestDNSAuthzDenyAndMissing(t *testing.T) {
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
	deny := http.NewServeMux()
	svc := &dns.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(deny, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})
	base := "/dns/v1/projects/" + project + "/managedZones"
	req := httptest.NewRequest(http.MethodPost, base, bytes.NewReader([]byte(`{"name":"z","dnsName":"z.com."}`)))
	rec := httptest.NewRecorder()
	deny.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("create deny: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, base+"/z", nil)
	rec = httptest.NewRecorder()
	deny.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("get deny: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodDelete, base+"/z", nil)
	rec = httptest.NewRecorder()
	deny.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("delete deny: %d", rec.Code)
	}

	root := http.NewServeMux()
	svc2 := &dns.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc2.Mount(root, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@" + project + ".iam.gserviceaccount.com", IsRoot: true}, true
	})
	req = httptest.NewRequest(http.MethodGet, base+"/missing", nil)
	rec = httptest.NewRecorder()
	root.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing get: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodDelete, base+"/missing", nil)
	rec = httptest.NewRecorder()
	root.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing delete: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPost, base, bytes.NewReader([]byte(`not-json`)))
	rec = httptest.NewRecorder()
	root.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatal("bad json")
	}
}
