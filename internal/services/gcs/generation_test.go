package gcs_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGCSGenerationPreconditionsAndDownload(t *testing.T) {
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

	rec := do(http.MethodPost, "/storage/v1/b?project="+project, `{"name":"gen-b"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodPost, "/upload/storage/v1/b/gen-b/o?uploadType=media&name=g.txt", "gen-body", "text/plain")
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: %d", rec.Code)
	}
	var obj map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &obj)
	genStr := fmt.Sprint(obj["generation"])
	if genStr == "" || genStr == "<nil>" {
		t.Fatalf("no generation in %#v", obj)
	}

	rec = do(http.MethodGet, "/storage/v1/b/gen-b/o/g.txt?generation="+genStr, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get gen: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodGet, "/storage/v1/b/gen-b/o/g.txt?ifGenerationMatch="+genStr, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("match ok: %d", rec.Code)
	}
	rec = do(http.MethodGet, "/storage/v1/b/gen-b/o/g.txt?ifGenerationMatch=999999999", "", "")
	if rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("match fail: %d", rec.Code)
	}
	rec = do(http.MethodGet, "/storage/v1/b/gen-b/o/g.txt?ifGenerationMatch=bad", "", "")
	if rec.Code == http.StatusOK {
		t.Fatal("bad match")
	}
	rec = do(http.MethodGet, "/storage/v1/b/gen-b/o/g.txt?generation=bad", "", "")
	if rec.Code == http.StatusOK {
		t.Fatal("bad generation")
	}
	rec = do(http.MethodGet, "/storage/v1/b/gen-b/o/g.txt?alt=media", "", "")
	if rec.Code != http.StatusOK || rec.Body.String() != "gen-body" {
		t.Fatalf("media: %d %q", rec.Code, rec.Body.String())
	}
	rec = do(http.MethodDelete, "/storage/v1/b/gen-b/o/g.txt?ifGenerationMatch=999999999", "", "")
	if rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("delete mismatch: %d", rec.Code)
	}
	rec = do(http.MethodDelete, "/storage/v1/b/gen-b/o/g.txt?ifGenerationMatch="+genStr, "", "")
	if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("delete match: %d %s", rec.Code, rec.Body.String())
	}

	rec = do(http.MethodPost, "/storage/v1/b/gen-b/o/x:generateSignedUrl", `{"method":"HEAD"}`, "")
	if rec.Code == http.StatusOK {
		t.Fatal("bad signed method")
	}
	rec = do(http.MethodGet, "/storage/v1/b/no-bucket/o/x.txt", "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing bucket object: %d", rec.Code)
	}
}
