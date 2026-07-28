package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/config"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/server"
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

func TestCRMPatchProjectDisplayName(t *testing.T) {
	srv, cfg := testServer(t)
	payload := []byte(`{"displayName":"Noctaxris Lab"}`)
	req := httptest.NewRequest(http.MethodPatch, "/v3/projects/"+cfg.ProjectID+"?updateMask=displayName", bytes.NewReader(payload))
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
	if body["displayName"] != "Noctaxris Lab" {
		t.Fatalf("displayName = %#v", body["displayName"])
	}
}

func TestIAMCreateServiceAccount(t *testing.T) {
	srv, cfg := testServer(t)
	email := createLabServiceAccount(t, srv, cfg, "lab-runner", "Lab Runner")
	if email != "lab-runner@"+cfg.ProjectID+".iam.gserviceaccount.com" {
		t.Fatalf("email = %s", email)
	}
}

func TestIAMServiceAccountEnableDisablePatchAndIAM(t *testing.T) {
	srv, cfg := testServer(t)
	email := createLabServiceAccount(t, srv, cfg, "lab-toggle", "Toggle")

	disableReq := httptest.NewRequest(http.MethodPost, "/v1/projects/"+cfg.ProjectID+"/serviceAccounts/"+email+":disable", nil)
	disableReq.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	disableRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(disableRec, disableReq)
	if disableRec.Code != http.StatusOK {
		t.Fatalf("disable status = %d body=%s", disableRec.Code, disableRec.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/v1/projects/"+cfg.ProjectID+"/serviceAccounts/"+email, nil)
	getReq.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	getRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", getRec.Code, getRec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(getRec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["disabled"] != true {
		t.Fatalf("disabled = %#v", got["disabled"])
	}

	enableReq := httptest.NewRequest(http.MethodPost, "/v1/projects/"+cfg.ProjectID+"/serviceAccounts/"+email+":enable", nil)
	enableReq.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	enableRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(enableRec, enableReq)
	if enableRec.Code != http.StatusOK {
		t.Fatalf("enable status = %d body=%s", enableRec.Code, enableRec.Body.String())
	}

	patchPayload := []byte(`{"serviceAccount":{"displayName":"Toggle Renamed"},"updateMask":"displayName"}`)
	patchReq := httptest.NewRequest(http.MethodPatch, "/v1/projects/"+cfg.ProjectID+"/serviceAccounts/"+email, bytes.NewReader(patchPayload))
	patchReq.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	patchReq.Header.Set("Content-Type", "application/json")
	patchRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(patchRec, patchReq)
	if patchRec.Code != http.StatusOK {
		t.Fatalf("patch status = %d body=%s", patchRec.Code, patchRec.Body.String())
	}
	var patched map[string]any
	if err := json.Unmarshal(patchRec.Body.Bytes(), &patched); err != nil {
		t.Fatal(err)
	}
	if patched["displayName"] != "Toggle Renamed" || patched["disabled"] != false {
		t.Fatalf("patched = %#v", patched)
	}

	setPayload := []byte(`{"policy":{"bindings":[{"role":"roles/viewer","members":["serviceAccount:viewer@` + cfg.ProjectID + `.iam.gserviceaccount.com"]}],"etag":"sa1"}}`)
	setReq := httptest.NewRequest(http.MethodPost, "/v1/projects/"+cfg.ProjectID+"/serviceAccounts/"+email+":setIamPolicy", bytes.NewReader(setPayload))
	setReq.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	setReq.Header.Set("Content-Type", "application/json")
	setRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(setRec, setReq)
	if setRec.Code != http.StatusOK {
		t.Fatalf("setIamPolicy status = %d body=%s", setRec.Code, setRec.Body.String())
	}

	getPolReq := httptest.NewRequest(http.MethodPost, "/v1/projects/"+cfg.ProjectID+"/serviceAccounts/"+email+":getIamPolicy", bytes.NewReader([]byte("{}")))
	getPolReq.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	getPolReq.Header.Set("Content-Type", "application/json")
	getPolRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(getPolRec, getPolReq)
	if getPolRec.Code != http.StatusOK {
		t.Fatalf("getIamPolicy status = %d body=%s", getPolRec.Code, getPolRec.Body.String())
	}

	testPayload := []byte(`{"permissions":["iam.serviceAccounts.get","storage.buckets.create"]}`)
	testReq := httptest.NewRequest(http.MethodPost, "/v1/projects/"+cfg.ProjectID+"/serviceAccounts/"+email+":testIamPermissions", bytes.NewReader(testPayload))
	testReq.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	testReq.Header.Set("Content-Type", "application/json")
	testRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(testRec, testReq)
	if testRec.Code != http.StatusOK {
		t.Fatalf("testIamPermissions status = %d body=%s", testRec.Code, testRec.Body.String())
	}
	var testBody map[string]any
	if err := json.Unmarshal(testRec.Body.Bytes(), &testBody); err != nil {
		t.Fatal(err)
	}
	perms, _ := testBody["permissions"].([]any)
	if len(perms) != 2 {
		t.Fatalf("root testIamPermissions = %#v", testBody)
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

func TestServiceUsageBatchEnableAndFilter(t *testing.T) {
	srv, cfg := testServer(t)

	disableReq := httptest.NewRequest(http.MethodPost, "/v1/projects/"+cfg.ProjectID+"/services/storage.googleapis.com:disable", nil)
	disableReq.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	disableRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(disableRec, disableReq)
	if disableRec.Code != http.StatusOK {
		t.Fatalf("disable status = %d body=%s", disableRec.Code, disableRec.Body.String())
	}

	batchPayload := []byte(`{"serviceIds":["storage.googleapis.com","pubsub.googleapis.com"]}`)
	batchReq := httptest.NewRequest(http.MethodPost, "/v1/projects/"+cfg.ProjectID+"/services:batchEnable", bytes.NewReader(batchPayload))
	batchReq.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	batchReq.Header.Set("Content-Type", "application/json")
	batchRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(batchRec, batchReq)
	if batchRec.Code != http.StatusOK {
		t.Fatalf("batchEnable status = %d body=%s", batchRec.Code, batchRec.Body.String())
	}
	var batchBody map[string]any
	if err := json.Unmarshal(batchRec.Body.Bytes(), &batchBody); err != nil {
		t.Fatal(err)
	}
	if batchBody["done"] != true {
		t.Fatalf("batchEnable = %#v", batchBody)
	}

	filterReq := httptest.NewRequest(http.MethodGet, "/v1/projects/"+cfg.ProjectID+"/services?filter=state:ENABLED", nil)
	filterReq.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	filterRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(filterRec, filterReq)
	if filterRec.Code != http.StatusOK {
		t.Fatalf("filter status = %d body=%s", filterRec.Code, filterRec.Body.String())
	}
	var filterBody map[string]any
	if err := json.Unmarshal(filterRec.Body.Bytes(), &filterBody); err != nil {
		t.Fatal(err)
	}
	services, _ := filterBody["services"].([]any)
	if len(services) < 1 {
		t.Fatalf("enabled services empty: %s", filterRec.Body.String())
	}
	for _, raw := range services {
		svc, _ := raw.(map[string]any)
		if svc["state"] != "ENABLED" {
			t.Fatalf("non-enabled in filter: %#v", svc)
		}
	}
}

func createLabServiceAccount(t *testing.T, srv *server.Server, cfg config.Config, accountID, displayName string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"accountId": accountID,
		"serviceAccount": map[string]any{
			"displayName": displayName,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+cfg.ProjectID+"/serviceAccounts", bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create SA status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	email, _ := body["email"].(string)
	if email == "" {
		t.Fatalf("missing email in %#v", body)
	}
	if body["displayName"] != displayName {
		t.Fatalf("displayName = %#v", body["displayName"])
	}
	return email
}
