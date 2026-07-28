package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/filestore"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/vertexai"
)

func TestFilestoreViaServer(t *testing.T) {
	srv, cfg := testServer(t)
	token := cfg.RootAccessToken
	project := cfg.ProjectID
	loc := filestore.DefaultLocation
	base := "/file/v1/projects/" + project + "/locations/" + loc + "/instances"
	req := httptest.NewRequest(http.MethodPost, base+"?instanceId=srv-nfs", bytes.NewReader([]byte(
		`{"tier":"BASIC_HDD","fileShares":[{"name":"vol1","capacityGb":"1024"}],"networks":[{"network":"default","modes":["MODE_IPV4"]}]}`,
	)))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var inst map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &inst)
	if inst["state"] != "READY" {
		t.Fatalf("instance=%#v", inst)
	}

	req = httptest.NewRequest(http.MethodGet, base+"/srv-nfs", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var list map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	items, _ := list["instances"].([]any)
	if len(items) != 1 {
		t.Fatalf("list=%#v", list)
	}

	req = httptest.NewRequest(http.MethodDelete, base+"/srv-nfs", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base+"/srv-nfs", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get after delete status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestVertexAIViaServer(t *testing.T) {
	srv, cfg := testServer(t)
	token := cfg.RootAccessToken
	project := cfg.ProjectID
	path := "/v1/projects/" + project + "/locations/" + vertexai.DefaultLocation +
		"/publishers/google/models/gemini-2.0-flash:generateContent"
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(
		`{"contents":[{"role":"user","parts":[{"text":"ping"}]}]}`,
	)))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("generateContent status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	cands, _ := body["candidates"].([]any)
	if len(cands) != 1 {
		t.Fatalf("body=%#v", body)
	}

	bad := "/v1/projects/" + project + "/locations/" + vertexai.DefaultLocation +
		"/publishers/google/models/unknown-lab-model:generateContent"
	req = httptest.NewRequest(http.MethodPost, bad, bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown model, got %d body=%s", rec.Code, rec.Body.String())
	}
}
