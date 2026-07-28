package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCloudArmorViaServer(t *testing.T) {
	srv, cfg := testServer(t)
	h := srv.Handler()
	auth := func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	}
	base := "/compute/v1/projects/" + cfg.ProjectID + "/global/securityPolicies"

	req := httptest.NewRequest(http.MethodPost, base, bytes.NewReader([]byte(`{"name":"srv-armor"}`)))
	auth(req)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("insert status=%d body=%s", rec.Code, rec.Body.String())
	}

	rule := `{"priority":50,"action":"deny(403)","match":{"byteMatchSet":{"fieldToMatch":"UriPath","positionalConstraint":"CONTAINS","searchString":"/secret"}}}`
	req = httptest.NewRequest(http.MethodPost, base+"/srv-armor/addRule", bytes.NewReader([]byte(rule)))
	auth(req)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("addRule status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, base+"/srv-armor:validate",
		bytes.NewReader([]byte(`{"uriPath":"/secret/keys"}`)))
	auth(req)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("validate status=%d body=%s", rec.Code, rec.Body.String())
	}
	var result map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &result)
	if result["allowed"] != false {
		t.Fatalf("expected deny: %#v", result)
	}
}

func TestCertificateManagerViaServer(t *testing.T) {
	srv, cfg := testServer(t)
	h := srv.Handler()
	auth := func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	}
	base := "/v1/projects/" + cfg.ProjectID + "/locations/global/certificates"
	req := httptest.NewRequest(http.MethodPost, base+"?certificateId=srv-cert", bytes.NewReader([]byte(
		`{"description":"srv","selfManaged":{"pemCertificate":"PEMlab","pemPrivateKey":"KEYlab"}}`,
	)))
	auth(req)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create cert status=%d body=%s", rec.Code, rec.Body.String())
	}
	var createOp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &createOp)
	if createOp["done"] != true {
		t.Fatalf("create expected done Operation: %#v", createOp)
	}
	cert, _ := createOp["response"].(map[string]any)
	if cert == nil {
		t.Fatalf("create missing response: %#v", createOp)
	}
	if sm, _ := cert["selfManaged"].(map[string]any); sm != nil {
		if sm["pemPrivateKey"] != nil {
			t.Fatalf("private key must not be returned: %#v", sm)
		}
	}

	req = httptest.NewRequest(http.MethodGet, base+"/srv-cert", nil)
	auth(req)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get cert status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got["name"] == nil || got["name"] == "" {
		t.Fatalf("get cert missing name: %#v", got)
	}
	if sm, _ := got["selfManaged"].(map[string]any); sm != nil {
		if sm["pemPrivateKey"] != nil {
			t.Fatalf("get must not return private key: %#v", sm)
		}
	}

	mapBase := "/v1/projects/" + cfg.ProjectID + "/locations/global/certificateMaps"
	req = httptest.NewRequest(http.MethodPost, mapBase+"?certificateMapId=srv-map", bytes.NewReader([]byte(`{"description":"map"}`)))
	auth(req)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create map status=%d body=%s", rec.Code, rec.Body.String())
	}
	var mapOp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &mapOp)
	if mapOp["done"] != true {
		t.Fatalf("create map expected done Operation: %#v", mapOp)
	}

	req = httptest.NewRequest(http.MethodGet, mapBase+"/srv-map", nil)
	auth(req)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get map status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCloudArmorAuthzUnauthenticated(t *testing.T) {
	srv, cfg := testServer(t)
	path := "/compute/v1/projects/" + cfg.ProjectID + "/global/securityPolicies"
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
