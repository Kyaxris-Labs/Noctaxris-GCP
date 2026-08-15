package gcs_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGCSErrorAndEdgeBranches(t *testing.T) {
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

	rec := do(http.MethodPost, "/storage/v1/b", `{"name":"no-proj"}`, "")
	if rec.Code == http.StatusOK {
		t.Fatalf("create without project should fail: %d %s", rec.Code, rec.Body.String())
	}

	rec = do(http.MethodPost, "/storage/v1/b?project="+project, `not-json`, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad json create: %d", rec.Code)
	}

	rec = do(http.MethodGet, "/storage/v1/b/missing-bucket", "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing bucket: %d", rec.Code)
	}

	rec = do(http.MethodDelete, "/storage/v1/b/missing-bucket", "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing: %d", rec.Code)
	}

	rec = do(http.MethodPost, "/storage/v1/b?project="+project, `{"name":"edge-b"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}

	rec = do(http.MethodPost, "/upload/storage/v1/b/edge-b/o?uploadType=media&name=o1.txt", "one", "text/plain")
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: %d", rec.Code)
	}
	rec = do(http.MethodPost, "/upload/storage/v1/b/edge-b/o?uploadType=media&name=o2.txt", "two", "text/plain")
	if rec.Code != http.StatusOK {
		t.Fatalf("upload2: %d", rec.Code)
	}

	rec = do(http.MethodGet, "/storage/v1/b/edge-b/o?prefix=o", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list prefix: %d", rec.Code)
	}

	rec = do(http.MethodGet, "/storage/v1/b/edge-b/o/nope.txt", "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing object: %d", rec.Code)
	}
	rec = do(http.MethodDelete, "/storage/v1/b/edge-b/o/nope.txt", "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing obj: %d", rec.Code)
	}

	rec = do(http.MethodPost, "/storage/v1/b/edge-b/o/out/compose", `{"sourceObjects":[]}`, "")
	if rec.Code == http.StatusOK {
		t.Fatalf("empty compose should fail: %d", rec.Code)
	}

	rec = do(http.MethodPost, "/storage/v1/b/edge-b/o/o1.txt/copyTo/b/no-such-b/o/x.txt", `{}`, "")
	if rec.Code == http.StatusOK {
		t.Fatalf("copy missing dest: %d", rec.Code)
	}
	rec = do(http.MethodPost, "/storage/v1/b/edge-b/o/o1.txt/rewriteTo/b/no-such-b/o/x.txt", `{}`, "")
	if rec.Code == http.StatusOK {
		t.Fatalf("rewrite missing dest: %d", rec.Code)
	}

	rec = do(http.MethodPost, "/storage/v1/b/edge-b/o/o1.txt/rewriteTo/b/", `{}`, "")
	if rec.Code == http.StatusOK {
		t.Fatalf("bad rewrite path: %d", rec.Code)
	}

	rec = do(http.MethodPost, "/storage/v1/b/edge-b/notificationConfigs", `{"topic":"projects/`+project+`/topics/missing","payload_format":"JSON_API_V1"}`, "")
	if rec.Code == http.StatusOK {
		// may succeed store-side without topic existence; accept either
		_ = rec
	}

	rec = do(http.MethodGet, "/storage/v1/b/edge-b/notificationConfigs/999", "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing notification: %d", rec.Code)
	}
	rec = do(http.MethodDelete, "/storage/v1/b/edge-b/notificationConfigs/999", "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing notification: %d", rec.Code)
	}

	rec = do(http.MethodPost, "/upload/storage/v1/b/edge-b/o?uploadType=media", "x", "text/plain")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("media without name: %d", rec.Code)
	}

	rec = do(http.MethodPost, "/upload/storage/v1/b/edge-b/o?uploadType=resumable", `{}`, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("resumable without name: %d %s", rec.Code, rec.Body.String())
	}

	rec = do(http.MethodPut, "/upload/storage/v1/b/edge-b/o?uploadType=media&name=put.txt", "put-body", "text/plain")
	if rec.Code != http.StatusOK {
		t.Fatalf("put media: %d %s", rec.Code, rec.Body.String())
	}

	rec = do(http.MethodDelete, "/storage/v1/b/edge-b", "", "")
	if rec.Code == http.StatusOK || rec.Code == http.StatusNoContent {
		t.Fatalf("non-empty delete should fail: %d", rec.Code)
	}

	for _, o := range []string{"o1.txt", "o2.txt", "put.txt"} {
		_ = do(http.MethodDelete, "/storage/v1/b/edge-b/o/"+o, "", "")
	}
	rec = do(http.MethodDelete, "/storage/v1/b/edge-b", "", "")
	if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("delete empty: %d %s", rec.Code, rec.Body.String())
	}
}
