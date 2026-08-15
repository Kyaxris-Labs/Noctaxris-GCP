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

func TestGCSAuthzDenyAndDeleteBucketLifecycle(t *testing.T) {
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
	h := &gcshandler.Handler{
		Store: st, Authz: &authz.Evaluator{Policies: st}, DefaultProject: project,
		Principal: func(*http.Request) (authn.Principal, bool) {
			return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
		},
	}
	h.Register(mux)
	req := httptest.NewRequest(http.MethodGet, "/storage/v1/b?project="+project, nil)
	req.Host = "127.0.0.1:4588"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("list deny: %d %s", rec.Code, rec.Body.String())
	}

	mux2, _, _ := openGCS(t)
	host := "127.0.0.1:4588"
	do := func(method, path, body, ct string) *httptest.ResponseRecorder {
		t.Helper()
		var req *http.Request
		if body == "" {
			req = httptest.NewRequest(method, path, nil)
		} else {
			req = httptest.NewRequest(method, path, strings.NewReader(body))
			if ct == "" {
				ct = "application/json"
			}
			req.Header.Set("Content-Type", ct)
		}
		req.Host = host
		rec := httptest.NewRecorder()
		mux2.ServeHTTP(rec, req)
		return rec
	}
	rec = do(http.MethodPost, "/storage/v1/b?project="+project, `{"name":"del-life"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPost, "/upload/storage/v1/b/del-life/o?uploadType=media&name=x.txt", "x", "text/plain")
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: %d", rec.Code)
	}
	rec = do(http.MethodDelete, "/storage/v1/b/del-life", "", "")
	if rec.Code == http.StatusOK {
		t.Fatal("delete non-empty bucket should fail")
	}
	rec = do(http.MethodDelete, "/storage/v1/b/del-life/o/x.txt", "", "")
	if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("delete object: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodDelete, "/storage/v1/b/del-life", "", "")
	if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("delete empty bucket: %d %s", rec.Code, rec.Body.String())
	}

	rec = do(http.MethodPost, "/storage/v1/b?project="+project, `{"name":"notify-b"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("create notify bucket: %d", rec.Code)
	}
	rec = do(http.MethodPost, "/storage/v1/b/notify-b/notificationConfigs",
		`{"topic":"projects/`+project+`/topics/gcs-n","payload_format":"JSON_API_V1","event_types":["OBJECT_FINALIZE"]}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("insert notification: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodGet, "/storage/v1/b/notify-b/notificationConfigs", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list notifications: %d %s", rec.Code, rec.Body.String())
	}
}
