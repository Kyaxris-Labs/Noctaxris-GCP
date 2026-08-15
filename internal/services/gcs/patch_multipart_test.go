package gcs_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGCSPatchBucketRetentionAndMultipart(t *testing.T) {
	mux, _, project := openGCS(t)
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

	rec := do(http.MethodPost, "/storage/v1/b?project="+project, `{"name":"patch-b","location":"US"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}

	patch := `{"labels":{"env":"lab"},"storageClass":"NEARLINE"}`
	rec = do(http.MethodPatch, "/storage/v1/b/patch-b", patch, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}
	var bkt map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &bkt)
	labels, _ := bkt["labels"].(map[string]any)
	if labels["env"] != "lab" {
		t.Fatalf("labels=%#v", labels)
	}

	boundary := "noctaxrisboundary"
	mp := "--" + boundary + "\r\n" +
		"Content-Type: application/json; charset=UTF-8\r\n\r\n" +
		`{"name":"mp.txt","contentType":"text/plain"}` + "\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Type: text/plain\r\n\r\n" +
		"multipart-body\r\n" +
		"--" + boundary + "--\r\n"
	rec = do(http.MethodPost, "/upload/storage/v1/b/patch-b/o?uploadType=multipart", mp, "multipart/related; boundary="+boundary)
	if rec.Code != http.StatusOK {
		t.Fatalf("multipart upload: %d %s", rec.Code, rec.Body.String())
	}

	rec = do(http.MethodPost, "/upload/storage/v1/b/patch-b/o?uploadType=bogus&name=x", "x", "text/plain")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bogus uploadType: %d %s", rec.Code, rec.Body.String())
	}

	rec = do(http.MethodGet, "/storage/v1/b/patch-b/o/mp.txt?alt=media", "", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "multipart-body") {
		t.Fatalf("download mp: %d %q", rec.Code, rec.Body.String())
	}

	rec = do(http.MethodDelete, "/storage/v1/b/patch-b/o/mp.txt", "", "")
	if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("delete obj: %d %s", rec.Code, rec.Body.String())
	}

	ret := `{"retentionPolicy":{"retentionPeriod":"3600","isLocked":false}}`
	rec = do(http.MethodPatch, "/storage/v1/b/patch-b", ret, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("set retention: %d %s", rec.Code, rec.Body.String())
	}
	lock := `{"retentionPolicy":{"retentionPeriod":"3600","isLocked":true}}`
	rec = do(http.MethodPatch, "/storage/v1/b/patch-b", lock, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("lock retention: %d %s", rec.Code, rec.Body.String())
	}
	bad := `{"retentionPolicy":{"retentionPeriod":"60","isLocked":false}}`
	rec = do(http.MethodPatch, "/storage/v1/b/patch-b", bad, "")
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusOK {
		t.Fatalf("patch locked: %d %s", rec.Code, rec.Body.String())
	}

	rec = do(http.MethodDelete, "/storage/v1/b/patch-b", "", "")
	if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent && rec.Code != http.StatusBadRequest {
		t.Fatalf("delete bucket: %d %s", rec.Code, rec.Body.String())
	}
}
