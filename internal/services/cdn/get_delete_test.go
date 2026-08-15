package cdn_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cdn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestCDNGetDeleteAndHeadEdge(t *testing.T) {
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
	if _, _, err := st.CreateBucket("cdn-b", project, "US", "STANDARD"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PutObjectBytes("cdn-b", "x/y.txt", "text/plain", []byte("edge")); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	svc := &cdn.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@" + project + ".iam.gserviceaccount.com", IsRoot: true}, true
	})

	body := `{"name":"d1","origin":{"gcs":{"bucket":"cdn-b","objectPrefix":"x"}},"enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/global/distributions?distributionId=d1", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/global/distributions/d1", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodHead, "/cdn/d1/y.txt", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("head edge: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/cdn/d1/missing.txt", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusOK {
		t.Fatalf("missing edge: %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodDelete, "/v1/projects/"+project+"/global/distributions/d1", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
}
