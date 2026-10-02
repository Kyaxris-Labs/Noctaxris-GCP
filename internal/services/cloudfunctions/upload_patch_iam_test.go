package cloudfunctions_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudfunctions"
)

func TestCloudFunctionsUploadDownloadPatchAndIAM(t *testing.T) {
	mux, _ := mountFunctions(t, nil)
	loc := cloudfunctions.DefaultLocation
	project := "noctaxris-gcp-local"
	base := "/v2/projects/" + project + "/locations/" + loc + "/functions"

	req := httptest.NewRequest(http.MethodPost, base+"?functionId=", bytes.NewReader([]byte(`{}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing functionId status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, base+":generateUploadUrl", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("generateUploadUrl status=%d body=%s", rec.Code, rec.Body.String())
	}
	var up map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &up); err != nil {
		t.Fatal(err)
	}
	src, _ := up["storageSource"].(map[string]any)
	object, _ := src["object"].(string)
	if object == "" {
		t.Fatalf("upload source=%#v", up)
	}
	parts := bytes.Split([]byte(object), []byte("/"))
	uploadID := string(parts[len(parts)-1])
	if len(uploadID) > 4 && uploadID[len(uploadID)-4:] == ".zip" {
		uploadID = uploadID[:len(uploadID)-4]
	}

	req = httptest.NewRequest(http.MethodPut, base+":upload/"+uploadID, bytes.NewReader([]byte("zip-bytes")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("acceptUpload status=%d body=%s", rec.Code, rec.Body.String())
	}

	createBody := `{"labResponse":"{\"v\":1}","buildConfig":{"source":{"storageSource":{"bucket":"noctaxris-gcp-functions-lab","object":"` + object + `"}}}}`
	req = httptest.NewRequest(http.MethodPost, base+"?functionId=src-fn", bytes.NewReader([]byte(createBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create with source status=%d body=%s", rec.Code, rec.Body.String())
	}
	var fn map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &fn)
	if fn["state"] != "ACTIVE" {
		t.Fatalf("expected ACTIVE after prior upload, got %#v", fn["state"])
	}

	req = httptest.NewRequest(http.MethodPost, base+"?functionId=src-fn", bytes.NewReader([]byte(`{}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate create status=%d", rec.Code)
	}

	pendingObj := "uploads/" + project + "/pending-not-uploaded.zip"
	pendingBody := `{"labResponse":{"ok":true},"buildConfig":{"source":{"storageSource":{"bucket":"noctaxris-gcp-functions-lab","object":"` + pendingObj + `"}}}}`
	req = httptest.NewRequest(http.MethodPost, base+"?functionId=pending-fn", bytes.NewReader([]byte(pendingBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create pending status=%d body=%s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &fn)
	if fn["state"] != "DEPLOYING" {
		t.Fatalf("expected DEPLOYING, got %#v", fn["state"])
	}

	req = httptest.NewRequest(http.MethodGet, base+"/missing-fn", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing get status=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, base+"/src-fn:generateDownloadUrl", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("generateDownloadUrl status=%d body=%s", rec.Code, rec.Body.String())
	}
	var dl map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &dl)
	if dl["downloadUrl"] == nil {
		t.Fatalf("download=%#v", dl)
	}

	req = httptest.NewRequest(http.MethodPost, base+"/nope:generateDownloadUrl", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("download missing status=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPatch, base+"/src-fn", bytes.NewReader([]byte(`{"labResponse":{"patched":true},"description":"x"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPatch, base+"/missing-fn", bytes.NewReader([]byte(`{}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("patch missing status=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, base+"/src-fn:getIamPolicy", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("getIam empty status=%d body=%s", rec.Code, rec.Body.String())
	}

	pol := `{"policy":{"bindings":[{"role":"roles/cloudfunctions.invoker","members":["allUsers"]}],"etag":"ACAB"}}`
	req = httptest.NewRequest(http.MethodPost, base+"/src-fn:setIamPolicy", bytes.NewReader([]byte(pol)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setIam status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base+"/src-fn:getIamPolicy", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte("cloudfunctions.invoker")) {
		t.Fatalf("getIam after set status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, base+"/missing-fn:setIamPolicy", bytes.NewReader([]byte(pol)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("setIam missing status=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, base+"/src-fn:setIamPolicy", bytes.NewReader([]byte(`{`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("setIam bad json status=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, base, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodDelete, base+"/missing-fn", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing status=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, base+"/src-fn:unknown", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown method status=%d", rec.Code)
	}
}
