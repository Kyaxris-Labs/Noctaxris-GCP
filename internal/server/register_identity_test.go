package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCRMGetProject(t *testing.T) {
	srv, cfg := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v3/projects/"+cfg.ProjectID, nil)
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["projectId"] != cfg.ProjectID {
		t.Fatalf("projectId = %#v", body["projectId"])
	}
	if body["name"] != "projects/"+cfg.ProjectID {
		t.Fatalf("name = %#v", body["name"])
	}
	if body["state"] != "ACTIVE" {
		t.Fatalf("state = %#v", body["state"])
	}
}

func TestIAMCreateServiceAccount(t *testing.T) {
	srv, cfg := testServer(t)
	payload := []byte(`{"accountId":"lab-runner","serviceAccount":{"displayName":"Lab Runner"}}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+cfg.ProjectID+"/serviceAccounts", bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	wantEmail := "lab-runner@" + cfg.ProjectID + ".iam.gserviceaccount.com"
	if body["email"] != wantEmail {
		t.Fatalf("email = %#v want %s", body["email"], wantEmail)
	}
	if body["displayName"] != "Lab Runner" {
		t.Fatalf("displayName = %#v", body["displayName"])
	}
}

func TestServiceUsageList(t *testing.T) {
	srv, cfg := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/projects/"+cfg.ProjectID+"/services", nil)
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	services, _ := body["services"].([]any)
	if len(services) < 3 {
		t.Fatalf("services len = %d body=%s", len(services), rec.Body.String())
	}
}
