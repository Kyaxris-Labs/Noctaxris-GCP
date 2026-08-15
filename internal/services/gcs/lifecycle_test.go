package gcs_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	gcshandler "github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/gcs"
)

func TestGCSBucketObjectLifecycle(t *testing.T) {
	mux, st, project := openGCS(t)
	host := "127.0.0.1:4588"
	do := func(method, path, body, ct string) *httptest.ResponseRecorder {
		t.Helper()
		var req *http.Request
		if body == "" {
			req = httptest.NewRequest(method, path, nil)
		} else {
			req = httptest.NewRequest(method, path, strings.NewReader(body))
			if ct != "" {
				req.Header.Set("Content-Type", ct)
			} else {
				req.Header.Set("Content-Type", "application/json")
			}
		}
		req.Host = host
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	rec := do(http.MethodPost, "/storage/v1/b?project="+project, `{"name":"life-b","location":"US","storageClass":"STANDARD"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodGet, "/storage/v1/b?project="+project, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list buckets: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodGet, "/storage/v1/b/life-b", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get bucket: %d %s", rec.Code, rec.Body.String())
	}

	rec = do(http.MethodPost, "/upload/storage/v1/b/life-b/o?uploadType=media&name=a.txt", "alpha", "text/plain")
	if rec.Code != http.StatusOK {
		t.Fatalf("upload a: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPost, "/upload/storage/v1/b/life-b/o?uploadType=media&name=b.txt", "beta", "text/plain")
	if rec.Code != http.StatusOK {
		t.Fatalf("upload b: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodGet, "/storage/v1/b/life-b/o", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list objects: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodGet, "/storage/v1/b/life-b/o/a.txt", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get object meta: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodGet, "/storage/v1/b/life-b/o/a.txt?alt=media", "", "")
	if rec.Code != http.StatusOK || rec.Body.String() != "alpha" {
		t.Fatalf("download: %d %q", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPatch, "/storage/v1/b/life-b/o/a.txt", `{"contentType":"text/plain; charset=utf-8","metadata":{"k":"v"}}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("patch object: %d %s", rec.Code, rec.Body.String())
	}

	compose := `{"destination":{"contentType":"text/plain"},"sourceObjects":[{"name":"a.txt"},{"name":"b.txt"}]}`
	rec = do(http.MethodPost, "/storage/v1/b/life-b/o/composed.txt/compose", compose, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("compose: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPost, "/storage/v1/b/life-b/o/a.txt/copyTo/b/life-b/o/a-copy.txt", `{}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("copy: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPost, "/storage/v1/b/life-b/o/a.txt/rewriteTo/b/life-b/o/a-rewritten.txt", `{}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("rewrite: %d %s", rec.Code, rec.Body.String())
	}

	rec = do(http.MethodGet, "/storage/v1/b/life-b/iam", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get iam: %d %s", rec.Code, rec.Body.String())
	}
	iamBody := `{"bindings":[{"role":"roles/storage.objectViewer","members":["serviceAccount:viewer@example.com"]}]}`
	rec = do(http.MethodPut, "/storage/v1/b/life-b/iam", iamBody, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("set iam: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodGet, "/storage/v1/b/life-b/iam/testPermissions?permissions=storage.objects.get&permissions=storage.buckets.get", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("test iam: %d %s", rec.Code, rec.Body.String())
	}

	topic := "projects/" + project + "/topics/gcs-notify"
	if _, created, err := st.CreateTopic(topic, project); err != nil || !created {
		t.Fatalf("topic: created=%v err=%v", created, err)
	}
	rec = do(http.MethodPost, "/storage/v1/b/life-b/notificationConfigs", `{"topic":"`+topic+`","payload_format":"JSON_API_V1","event_types":["OBJECT_FINALIZE"]}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("insert notification: %d %s", rec.Code, rec.Body.String())
	}
	var ncfg map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &ncfg)
	nid, _ := ncfg["id"].(string)
	if nid == "" {
		t.Fatalf("notification=%#v", ncfg)
	}
	rec = do(http.MethodGet, "/storage/v1/b/life-b/notificationConfigs", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list notifications: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodGet, "/storage/v1/b/life-b/notificationConfigs/"+nid, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get notification: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodDelete, "/storage/v1/b/life-b/notificationConfigs/"+nid, "", "")
	if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("delete notification: %d %s", rec.Code, rec.Body.String())
	}

	rec = do(http.MethodDelete, "/storage/v1/b/life-b/o/a.txt", "", "")
	if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("delete object: %d %s", rec.Code, rec.Body.String())
	}
	for _, obj := range []string{"b.txt", "composed.txt", "a-copy.txt", "a-rewritten.txt"} {
		_ = do(http.MethodDelete, "/storage/v1/b/life-b/o/"+obj, "", "")
	}
	rec = do(http.MethodDelete, "/storage/v1/b/life-b", "", "")
	if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("delete bucket: %d %s", rec.Code, rec.Body.String())
	}
}

func TestGCSResumableUploadAndAuthzDeny(t *testing.T) {
	mux, st, project := openGCS(t)
	host := "127.0.0.1:4588"
	req := httptest.NewRequest(http.MethodPost, "/storage/v1/b?project="+project, strings.NewReader(`{"name":"resumable-b"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Host = host
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/upload/storage/v1/b/resumable-b/o?uploadType=resumable&name=big.bin", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Upload-Content-Type", "application/octet-stream")
	req.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Fatalf("initiate resumable: %d %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if loc == "" {
		t.Fatalf("missing Location header, headers=%v body=%s", rec.Header(), rec.Body.String())
	}
	uploadURL := loc
	if !strings.HasPrefix(uploadURL, "http") {
		uploadURL = "http://" + host + loc
	}
	put := httptest.NewRequest(http.MethodPut, uploadURL, strings.NewReader("chunk-data"))
	put.Header.Set("Content-Type", "application/octet-stream")
	put.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, put)
	if rec.Code != http.StatusOK {
		t.Fatalf("put resumable: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/upload/storage/v1/b/resumable-b/o?uploadType=resumable&name=cancel.bin", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	loc = rec.Header().Get("Location")
	if loc == "" {
		t.Fatalf("missing Location for cancel session")
	}
	delURL := loc
	if !strings.HasPrefix(delURL, "http") {
		delURL = "http://" + host + loc
	}
	del := httptest.NewRequest(http.MethodDelete, delURL, nil)
	del.Host = host
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, del)
	if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("delete resumable: %d %s", rec.Code, rec.Body.String())
	}

	denyMux := http.NewServeMux()
	h := &gcshandler.Handler{
		Store:          st,
		Authz:          &authz.Evaluator{Policies: st},
		DefaultProject: project,
		Principal: func(*http.Request) (authn.Principal, bool) {
			return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
		},
	}
	h.Register(denyMux)
	req = httptest.NewRequest(http.MethodGet, "/storage/v1/b?project="+project, nil)
	req.Host = host
	rec = httptest.NewRecorder()
	denyMux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("authz deny list: %d %s", rec.Code, rec.Body.String())
	}
}
