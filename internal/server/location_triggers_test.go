package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLocationTriggersSharedMux(t *testing.T) {
	srv, cfg := testServer(t)
	auth := "Bearer " + cfg.RootAccessToken
	project := cfg.ProjectID
	loc := "us-central1"
	base := "/v1/projects/" + project + "/locations/" + loc + "/triggers"

	req := httptest.NewRequest(http.MethodGet, base, nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth list: %d", rec.Code)
	}

	eaBody := `{"eventFilters":[{"attribute":"type","value":"google.cloud.pubsub.topic.v1.messagePublished"}],"destination":{"cloudRunService":{"service":"svc","region":"` + loc + `"}}}`
	req = httptest.NewRequest(http.MethodPost, base+"?triggerId=ea-shared", bytes.NewReader([]byte(eaBody)))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create eventarc: %d %s", rec.Code, rec.Body.String())
	}

	cbBody := `{"id":"cb-shared","filename":"cloudbuild.yaml"}`
	req = httptest.NewRequest(http.MethodPost, base, bytes.NewReader([]byte(cbBody)))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create cloudbuild: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base, nil)
	req.Header.Set("Authorization", auth)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var listed map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &listed)
	items, _ := listed["triggers"].([]any)
	if len(items) < 2 {
		t.Fatalf("list=%#v", listed)
	}

	req = httptest.NewRequest(http.MethodGet, base+"/ea-shared", nil)
	req.Header.Set("Authorization", auth)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get ea: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, base+"/cb-shared", nil)
	req.Header.Set("Authorization", auth)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get cb: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, base+"/missing", nil)
	req.Header.Set("Authorization", auth)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get missing: %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodDelete, base+"/ea-shared", nil)
	req.Header.Set("Authorization", auth)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete ea: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodDelete, base+"/cb-shared", nil)
	req.Header.Set("Authorization", auth)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete cb: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodDelete, base+"/missing", nil)
	req.Header.Set("Authorization", auth)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing: %d", rec.Code)
	}
}
