package appengine_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAppEnginePatchTrafficAndListInstances(t *testing.T) {
	mux := mountAppEngine(t)
	appID := "noctaxris-gcp-local"

	req := httptest.NewRequest(http.MethodPost, "/v1/apps", bytes.NewReader([]byte(`{"id":"`+appID+`","locationId":"us-central"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK && rec.Code != http.StatusConflict {
		t.Fatalf("create app status=%d body=%s", rec.Code, rec.Body.String())
	}

	verURL := "/v1/apps/" + appID + "/services/default/versions"
	req = httptest.NewRequest(http.MethodPost, verURL, bytes.NewReader([]byte(`{"id":"v1","runtime":"go121"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create version status=%d body=%s", rec.Code, rec.Body.String())
	}

	patchURL := "/v1/apps/" + appID + "/services/default?migrateTraffic=true"
	req = httptest.NewRequest(http.MethodPatch, patchURL, bytes.NewReader([]byte(
		`{"split":{"allocations":{"v1":1}},"shardBy":"IP"}`,
	)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch service status=%d body=%s", rec.Code, rec.Body.String())
	}
	var svc map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &svc)
	if svc["id"] != "default" && svc["name"] == nil {
		t.Fatalf("patched service=%#v", svc)
	}

	req = httptest.NewRequest(http.MethodPatch, "/v1/apps/"+appID+"/services/missing",
		bytes.NewReader([]byte(`{"split":{}}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("patch missing status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPatch, "/v1/apps/"+appID+"/services/default",
		bytes.NewReader([]byte(`{`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("patch bad json status=%d body=%s", rec.Code, rec.Body.String())
	}

	instURL := "/v1/apps/" + appID + "/services/default/versions/v1/instances"
	req = httptest.NewRequest(http.MethodGet, instURL, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list instances status=%d body=%s", rec.Code, rec.Body.String())
	}
	var inst map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &inst)
	if _, ok := inst["instances"]; !ok {
		t.Fatalf("instances body=%#v", inst)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/apps/"+appID+"/services/default/versions/missing/instances", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing version instances status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/apps/"+appID+"/services/default/versions", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list versions status=%d body=%s", rec.Code, rec.Body.String())
	}
}
