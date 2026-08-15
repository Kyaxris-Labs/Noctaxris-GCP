package bigquery_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/bigquery"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestBigQueryAuthzDenyAndMissing(t *testing.T) {
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
	svc := &bigquery.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})
	base := "/bigquery/v2/projects/" + project
	calls := []struct {
		method, path, body string
	}{
		{http.MethodPost, base+"/datasets", `{"datasetReference":{"datasetId":"d"}}`},
		{http.MethodGet, base+"/datasets/d", ""},
		{http.MethodDelete, base+"/datasets/d", ""},
		{http.MethodPost, base+"/datasets/d/tables", `{"tableReference":{"tableId":"t"}}`},
		{http.MethodGet, base+"/datasets/d/tables/t", ""},
		{http.MethodDelete, base+"/datasets/d/tables/t", ""},
		{http.MethodPost, base+"/datasets/d/tables/t/insertAll", `{"rows":[{"json":{"a":1}}]}`},
		{http.MethodPost, base+"/queries", `{"query":"SELECT 1"}`},
		{http.MethodGet, base+"/jobs/j1", ""},
	}
	for _, c := range calls {
		var req *http.Request
		if c.body == "" {
			req = httptest.NewRequest(c.method, c.path, nil)
		} else {
			req = httptest.NewRequest(c.method, c.path, bytes.NewReader([]byte(c.body)))
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s want 403 got %d %s", c.method, c.path, rec.Code, rec.Body.String())
		}
	}

	root, _ := testBigQueryMux(t)
	req := httptest.NewRequest(http.MethodGet, base+"/datasets/missing", nil)
	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing dataset: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, base+"/datasets/missing/tables/t", nil)
	rec = httptest.NewRecorder()
	root.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing table: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, base+"/jobs/missing", nil)
	rec = httptest.NewRecorder()
	root.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing job: %d", rec.Code)
	}
}
