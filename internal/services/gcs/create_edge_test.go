package gcs_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	gcshandler "github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/gcs"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestGCSCreateLabelsConflictUnauthDisabled(t *testing.T) {
	mux, st, project := openGCS(t)
	host := "127.0.0.1:4588"
	do := func(m *http.ServeMux, method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		var req *http.Request
		if body == "" {
			req = httptest.NewRequest(method, path, nil)
		} else {
			req = httptest.NewRequest(method, path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
		}
		req.Host = host
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, req)
		return rec
	}

	rec := do(mux, http.MethodPost, "/storage/v1/b?project="+project, `{"name":""}`)
	if rec.Code == http.StatusOK {
		t.Fatal("empty name should fail")
	}
	rec = do(mux, http.MethodGet, "/storage/v1/b", "")
	if rec.Code == http.StatusOK {
		t.Fatal("list without project should fail")
	}
	rec = do(mux, http.MethodPost, "/storage/v1/b?project="+project,
		`{"name":"lab-labels","labels":{"env":"lab"},"location":"US","storageClass":"STANDARD"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create with labels: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(mux, http.MethodPost, "/storage/v1/b?project="+project, `{"name":"lab-labels"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate want 409 got %d %s", rec.Code, rec.Body.String())
	}

	rec = do(mux, http.MethodPost, "/upload/storage/v1/b/lab-labels/o?uploadType=media&name=m.txt", "meta")
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: %d", rec.Code)
	}
	rec = do(mux, http.MethodPatch, "/storage/v1/b/lab-labels/o/m.txt",
		`{"contentType":"text/plain","metadata":{"k":"v"},"cacheControl":"no-cache","contentDisposition":"inline","contentEncoding":"identity","contentLanguage":"en"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch object: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(mux, http.MethodPatch, "/storage/v1/b/lab-labels/o/m.txt?generation=999999", `{"contentType":"text/plain"}`)
	if rec.Code == http.StatusOK {
		t.Fatal("bad generation patch should fail")
	}
	rec = do(mux, http.MethodPatch, "/storage/v1/b/missing-bkt", `{"location":"EU"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("patch missing bucket: %d", rec.Code)
	}
	rec = do(mux, http.MethodPatch, "/storage/v1/b/lab-labels", `{"retentionPolicy":{"retentionPeriod":"not-a-number"}}`)
	if rec.Code == http.StatusOK {
		t.Fatal("bad retention should fail")
	}

	if err := st.BatchDisableServiceUsage(project, []string{"storage.googleapis.com"}); err != nil {
		t.Fatal(err)
	}
	rec = do(mux, http.MethodPost, "/storage/v1/b?project="+project, `{"name":"disabled-b"}`)
	if rec.Code == http.StatusOK {
		t.Fatal("disabled storage create should fail")
	}
	_ = st.BatchEnableServiceUsage(project, []string{"storage.googleapis.com"})

	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st2, err := store.Open(filepath.Join(dir, "data"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st2.Close() })
	if err := st2.EnsureRoot(project, "root@"+project+".iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}
	muxUnauth := http.NewServeMux()
	h := &gcshandler.Handler{
		Store: st2, Authz: &authz.Evaluator{Policies: st2}, DefaultProject: project,
		Principal: func(*http.Request) (authn.Principal, bool) { return authn.Principal{}, false },
	}
	h.Register(muxUnauth)
	rec = do(muxUnauth, http.MethodGet, "/storage/v1/b?project="+project, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth list: %d %s", rec.Code, rec.Body.String())
	}
}
