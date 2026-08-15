package cloudfunctions_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudfunctions"
)

func TestCloudFunctionsListPatchUploadIAMGet(t *testing.T) {
	mux, _ := mountFunctions(t, nil)
	loc := cloudfunctions.DefaultLocation
	project := "noctaxris-gcp-local"
	base := "/v2/projects/" + project + "/locations/" + loc + "/functions"

	req := httptest.NewRequest(http.MethodPost, base+"?functionId=patch-fn", bytes.NewReader([]byte(`{"labResponse":{"v":1}}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var list map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	fns, _ := list["functions"].([]any)
	if len(fns) < 1 {
		t.Fatalf("functions=%#v", list)
	}

	req = httptest.NewRequest(http.MethodGet, base+"/patch-fn", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPatch, base+"/patch-fn", bytes.NewReader([]byte(`{"labResponse":{"v":2},"description":"updated"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, base+"/patch-fn:getIamPolicy", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("getIam: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, base+":generateUploadUrl", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("generateUploadUrl: %d %s", rec.Code, rec.Body.String())
	}
	var up map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &up)
	uploadURL, _ := up["uploadUrl"].(string)
	if uploadURL == "" {
		t.Fatalf("uploadUrl missing: %#v", up)
	}
	// Accept relative or absolute theatre URL.
	putURL := uploadURL
	if strings.HasPrefix(putURL, "/") {
		putURL = "http://127.0.0.1:4588" + putURL
	}
	req = httptest.NewRequest(http.MethodPut, putURL, bytes.NewReader([]byte("zip-bytes")))
	req.Header.Set("Content-Type", "application/zip")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("accept upload: %d %s url=%s", rec.Code, rec.Body.String(), putURL)
	}

	req = httptest.NewRequest(http.MethodDelete, base+"/patch-fn", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
}
