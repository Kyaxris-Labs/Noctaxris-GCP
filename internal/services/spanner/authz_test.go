package spanner_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/spanner"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestSpannerAuthzDenyAndMissing(t *testing.T) {
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
	svc := &spanner.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})
	inst := "/v1/projects/" + project + "/instances"
	calls := []struct {
		method, path, body string
	}{
		{http.MethodPost, inst, `{"instanceId":"x","instance":{"displayName":"x"}}`},
		{http.MethodGet, inst+"/x", ""},
		{http.MethodDelete, inst+"/x", ""},
		{http.MethodPost, inst+"/x/databases", `{"createStatement":"CREATE DATABASE \` + "`" + `d` + "`" + `"}`},
		{http.MethodGet, inst+"/x/databases/d", ""},
		{http.MethodDelete, inst+"/x/databases/d", ""},
		{http.MethodPost, inst+"/x/databases/d/sessions", `{}`},
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

	root := http.NewServeMux()
	svc2 := &spanner.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc2.Mount(root, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@" + project + ".iam.gserviceaccount.com", IsRoot: true}, true
	})
	req := httptest.NewRequest(http.MethodGet, inst+"/missing", nil)
	rec := httptest.NewRecorder()
	root.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing instance: %d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, inst+"/missing/databases/d", nil)
	rec = httptest.NewRecorder()
	root.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing db: %d", rec.Code)
	}
}
