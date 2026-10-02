package gcs_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGCSNotificationsCopyRewriteAndResumable(t *testing.T) {
	mux, _, project := openGCS(t)
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
		mux.ServeHTTP(rec, req)
		return rec
	}

	rec := do(http.MethodPost, "/storage/v1/b?project="+project, `{"name":"src-n"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("create src: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPost, "/storage/v1/b?project="+project, `{"name":"dst-n"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("create dst: %d %s", rec.Code, rec.Body.String())
	}

	topic := "//pubsub.googleapis.com/projects/" + project + "/topics/gcs-events"
	rec = do(http.MethodPost, "/storage/v1/b/src-n/notificationConfigs",
		`{"topic":"`+topic+`","payload_format":"JSON_API_V1","event_types":["OBJECT_FINALIZE"],"object_name_prefix":"p/","custom_attributes":{"k":"v"}}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("insert notification: %d %s", rec.Code, rec.Body.String())
	}
	var n map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &n); err != nil {
		t.Fatal(err)
	}
	nid, _ := n["id"].(string)
	if nid == "" {
		t.Fatalf("notification=%#v", n)
	}

	rec = do(http.MethodPost, "/storage/v1/b/src-n/notificationConfigs", `{"topic":"bad","payload_format":"JSON_API_V1"}`, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad topic: %d", rec.Code)
	}
	rec = do(http.MethodPost, "/storage/v1/b/src-n/notificationConfigs", `{"topic":"`+topic+`"}`, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing payload_format: %d", rec.Code)
	}
	rec = do(http.MethodPost, "/storage/v1/b/missing-n/notificationConfigs",
		`{"topic":"`+topic+`","payload_format":"JSON_API_V1"}`, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing bucket notify: %d", rec.Code)
	}

	rec = do(http.MethodGet, "/storage/v1/b/src-n/notificationConfigs", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list notify: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodGet, "/storage/v1/b/src-n/notificationConfigs/"+nid, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get notify: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodGet, "/storage/v1/b/src-n/notificationConfigs/nope", "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing notify: %d", rec.Code)
	}

	rec = do(http.MethodPost, "/upload/storage/v1/b/src-n/o?uploadType=media&name=p/a.txt", "alpha", "text/plain")
	if rec.Code != http.StatusOK {
		t.Fatalf("media upload: %d %s", rec.Code, rec.Body.String())
	}

	rec = do(http.MethodPost, "/storage/v1/b/src-n/o/p/a.txt/copyTo/b/dst-n/o/copied.txt", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("copy: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPost, "/storage/v1/b/src-n/o/p/a.txt/copyTo/b/dst-n/o/copied2.txt?sourceGeneration=notint", "", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad sourceGeneration: %d", rec.Code)
	}
	rec = do(http.MethodPost, "/storage/v1/b/src-n/o/missing.txt/copyTo/b/dst-n/o/x.txt", "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("copy missing: %d", rec.Code)
	}
	rec = do(http.MethodPost, "/storage/v1/b/src-n/o/p/a.txt/copyTo/b/no-dst/o/x.txt", "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("copy missing dst: %d", rec.Code)
	}

	rec = do(http.MethodPost, "/storage/v1/b/src-n/o/p/a.txt/rewriteTo/b/dst-n/o/rewritten.txt", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("rewrite: %d %s", rec.Code, rec.Body.String())
	}
	var rewrite map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &rewrite)
	if rewrite["done"] != true {
		t.Fatalf("rewrite=%#v", rewrite)
	}
	rec = do(http.MethodPost, "/storage/v1/b/src-n/o/p/a.txt/rewriteTo/b/dst-n/o/bad.txt?sourceGeneration=xx", "", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("rewrite bad gen: %d", rec.Code)
	}

	rec = do(http.MethodPost, "/upload/storage/v1/b/src-n/o?uploadType=resumable", `{"name":"r/res.txt","contentType":"text/plain"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("initiate resumable: %d %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if loc == "" {
		t.Fatal("missing Location")
	}
	rec = do(http.MethodPut, loc, "resumable-body", "text/plain")
	if rec.Code != http.StatusOK {
		t.Fatalf("put resumable: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPut, "/upload/storage/v1/b/src-n/o?uploadType=resumable&upload_id=missing", "x", "text/plain")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing upload session: %d", rec.Code)
	}
	rec = do(http.MethodPut, "/upload/storage/v1/b/src-n/o?uploadType=resumable", "x", "text/plain")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing upload_id: %d", rec.Code)
	}
	rec = do(http.MethodPut, "/upload/storage/v1/b/src-n/o?uploadType=media&name=put-media.txt", "via-put", "text/plain")
	if rec.Code != http.StatusOK {
		t.Fatalf("put media: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPut, "/upload/storage/v1/b/src-n/o?uploadType=media", "no-name", "text/plain")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("put media no name: %d", rec.Code)
	}

	rec = do(http.MethodPost, "/upload/storage/v1/b/src-n/o?uploadType=resumable", `{"name":"r/cancel.txt"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("initiate cancel: %d", rec.Code)
	}
	cancelLoc := rec.Header().Get("Location")
	rec = do(http.MethodDelete, cancelLoc, "", "")
	if rec.Code != http.StatusNoContent && rec.Code != http.StatusOK {
		t.Fatalf("delete resumable: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodDelete, "/upload/storage/v1/b/src-n/o?upload_id=gone", "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing session: %d", rec.Code)
	}

	rec = do(http.MethodDelete, "/storage/v1/b/src-n/notificationConfigs/"+nid, "", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete notify: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodDelete, "/storage/v1/b/src-n/notificationConfigs/"+nid, "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("second delete notify: %d", rec.Code)
	}
}

func TestGCSBucketIAMGetSetTest(t *testing.T) {
	mux, _, project := openGCS(t)
	host := "127.0.0.1:4588"
	do := func(method, path, body string) *httptest.ResponseRecorder {
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
		mux.ServeHTTP(rec, req)
		return rec
	}

	rec := do(http.MethodPost, "/storage/v1/b?project="+project, `{"name":"iam-b"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodGet, "/storage/v1/b/iam-b/iam", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get iam empty: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPut, "/storage/v1/b/iam-b/iam",
		`{"etag":"ACAB","bindings":[{"role":"roles/storage.objectViewer","members":["user:a@b.c"]}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("set iam: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodGet, "/storage/v1/b/iam-b/iam", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get iam: %d", rec.Code)
	}
	rec = do(http.MethodPut, "/storage/v1/b/iam-b/iam", `not-json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad iam: %d", rec.Code)
	}
	rec = do(http.MethodGet, "/storage/v1/b/iam-b/iam/testPermissions?permissions=storage.objects.get&permissions=storage.objects.create", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("testPermissions: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodGet, "/storage/v1/b/missing-iam/iam", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing get iam: %d", rec.Code)
	}
}
