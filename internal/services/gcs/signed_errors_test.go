package gcs_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGCSSignedURLAndObjectErrorBranches(t *testing.T) {
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

	rec := do(http.MethodPost, "/storage/v1/b?project="+project, `{"name":"sign-b"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPost, "/upload/storage/v1/b/sign-b/o?uploadType=media&name=a.txt", "data", "text/plain")
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: %d", rec.Code)
	}

	rec = do(http.MethodPost, "/storage/v1/b/missing/o/x.txt:generateSignedUrl", `{"method":"GET","expires":3600}`, "")
	if rec.Code == http.StatusOK {
		t.Fatal("signed URL on missing bucket should fail")
	}
	rec = do(http.MethodPost, "/storage/v1/b/sign-b/o/a.txt:generateSignedUrl", `{"method":"POST","expires":3600}`, "")
	if rec.Code == http.StatusOK {
		t.Fatal("POST signed URL should be rejected")
	}
	rec = do(http.MethodPost, "/storage/v1/b/sign-b/o/a.txt:generateSignedUrl", `{"method":"GET","expires":60}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("signed URL: %d %s", rec.Code, rec.Body.String())
	}
	var signed map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &signed)
	if signed["signedUrl"] == nil {
		t.Fatalf("signed=%#v", signed)
	}

	rec = do(http.MethodPatch, "/storage/v1/b/sign-b/o/missing.txt", `{"metadata":{"a":"1"}}`, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("patch missing: %d", rec.Code)
	}
	rec = do(http.MethodDelete, "/storage/v1/b/sign-b/o/missing.txt", "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing: %d", rec.Code)
	}
	rec = do(http.MethodPost, "/storage/v1/b/sign-b/o/missing.txt/copyTo/b/sign-b/o/y.txt", `{}`, "")
	if rec.Code == http.StatusOK {
		t.Fatal("copy missing source should fail")
	}
	rec = do(http.MethodPost, "/storage/v1/b/sign-b/o/missing.txt/rewriteTo/b/sign-b/o/y.txt", `{}`, "")
	if rec.Code == http.StatusOK {
		t.Fatal("rewrite missing source should fail")
	}

	rec = do(http.MethodPut, "/storage/v1/b/sign-b/iam", `{"bindings":[{"role":"roles/storage.objectViewer","members":["allUsers"]}]}`, "")
	if rec.Code == http.StatusOK {
		// public members may be rejected by org policy
		_ = rec.Code
	} else if rec.Code != http.StatusBadRequest && rec.Code != http.StatusForbidden {
		t.Fatalf("set public iam unexpected: %d %s", rec.Code, rec.Body.String())
	}

	rec = do(http.MethodGet, "/storage/v1/b/sign-b/o/a.txt?alt=media&ifGenerationMatch=1", "", "")
	_ = rec.Code
	rec = do(http.MethodGet, "/storage/v1/b/sign-b/o?delimiter=/&prefix=a", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list delimiter: %d", rec.Code)
	}

	rec = do(http.MethodDelete, "/storage/v1/b/missing-bucket/o/x.txt", "", "")
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusForbidden {
		t.Fatalf("delete in missing bucket: %d", rec.Code)
	}
	rec = do(http.MethodDelete, "/storage/v1/b/sign-b/o/a.txt?ifGenerationMatch=999999", "", "")
	if rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("delete precondition: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodDelete, "/storage/v1/b/sign-b/o/a.txt?generation=notint", "", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad generation: %d", rec.Code)
	}
}
