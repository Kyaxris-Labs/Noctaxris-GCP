package cdn_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cdn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestCDNEdgeGCS(t *testing.T) {
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
	if _, _, err := st.CreateBucket("edge-bucket", "noctaxris-gcp-local", "US", "STANDARD"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutObjectBytes("edge-bucket", "assets/app.js", "application/javascript", []byte("cdn-ok")); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	svc := &cdn.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	})

	project := "noctaxris-gcp-local"
	body := `{"name":"lab-cdn","origin":{"gcs":{"bucket":"edge-bucket","objectPrefix":"assets"}}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/global/distributions?distributionId=lab-cdn", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/cdn/lab-cdn/app.js", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("edge status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "cdn-ok" {
		t.Fatalf("body=%q", rec.Body.String())
	}
	if rec.Header().Get("X-Noctaxris-GCP-CDN") != "lab-cdn" {
		t.Fatalf("headers=%v", rec.Header())
	}
}

func TestCDNDistributionsList(t *testing.T) {
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
	if err := st.EnsureRoot("p", "root@p.iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	svc := &cdn.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@p.iam.gserviceaccount.com", IsRoot: true}, true
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/p/global/distributions?distributionId=d1",
		bytes.NewReader([]byte(`{"origin":{"gcs":{"bucket":"b"}}}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/projects/p/global/distributions", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list %d", rec.Code)
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	items, _ := out["distributions"].([]any)
	if len(items) != 1 {
		t.Fatalf("list=%#v", out)
	}
}

func TestCDNAuthzFailClosed(t *testing.T) {
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
	svc := &cdn.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/projects/noctaxris-gcp-local/global/distributions", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCDNNilAuthzFailClosed(t *testing.T) {
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
	svc := &cdn.Service{Store: st, Authz: nil}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/projects/noctaxris-gcp-local/global/distributions", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("nil Authz expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}
