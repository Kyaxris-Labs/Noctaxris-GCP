package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestCRMOrganizationAndFoldersViaServer(t *testing.T) {
	srv, cfg := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/v3/organizations/"+store.DefaultOrganizationID, nil)
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get org status=%d body=%s", rec.Code, rec.Body.String())
	}
	var org map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &org); err != nil {
		t.Fatal(err)
	}
	if org["name"] != store.DefaultOrganizationName {
		t.Fatalf("org name = %#v", org["name"])
	}

	createBody := []byte(`{"parent":"` + store.DefaultOrganizationName + `","displayName":"Team A"}`)
	req = httptest.NewRequest(http.MethodPost, "/v3/folders", bytes.NewReader(createBody))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create folder status=%d body=%s", rec.Code, rec.Body.String())
	}
	var folder map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &folder); err != nil {
		t.Fatal(err)
	}
	name, _ := folder["name"].(string)
	if name == "" || folder["displayName"] != "Team A" {
		t.Fatalf("folder = %#v", folder)
	}
	folderID := name[len("folders/"):]

	req = httptest.NewRequest(http.MethodGet, "/v3/folders?parent="+store.DefaultOrganizationName, nil)
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list folders status=%d body=%s", rec.Code, rec.Body.String())
	}
	var listBody map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &listBody)
	folders, _ := listBody["folders"].([]any)
	if len(folders) != 1 {
		t.Fatalf("list = %#v", listBody)
	}

	patchBody := []byte(`{"displayName":"Team B"}`)
	req = httptest.NewRequest(http.MethodPatch, "/v3/folders/"+folderID+"?updateMask=displayName", bytes.NewReader(patchBody))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch folder status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/v3/folders/"+folderID, nil)
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete folder status=%d body=%s", rec.Code, rec.Body.String())
	}
	var deleted map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &deleted)
	if deleted["state"] != "DELETE_REQUESTED" {
		t.Fatalf("deleted = %#v", deleted)
	}

	// Existing project get still works and reports org parent.
	req = httptest.NewRequest(http.MethodGet, "/v3/projects/"+cfg.ProjectID, nil)
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get project status=%d body=%s", rec.Code, rec.Body.String())
	}
	var proj map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &proj)
	if proj["parent"] != store.DefaultOrganizationName {
		t.Fatalf("project parent = %#v", proj["parent"])
	}
}

func TestCRMMoveFolderViaServer(t *testing.T) {
	srv, cfg := testServer(t)
	auth := "Bearer " + cfg.RootAccessToken

	createA := []byte(`{"parent":"` + store.DefaultOrganizationName + `","displayName":"Move Child"}`)
	req := httptest.NewRequest(http.MethodPost, "/v3/folders", bytes.NewReader(createA))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create child status=%d body=%s", rec.Code, rec.Body.String())
	}
	var folderA map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &folderA)
	idA := folderA["name"].(string)[len("folders/"):]

	createB := []byte(`{"parent":"` + store.DefaultOrganizationName + `","displayName":"Move Parent"}`)
	req = httptest.NewRequest(http.MethodPost, "/v3/folders", bytes.NewReader(createB))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create parent status=%d body=%s", rec.Code, rec.Body.String())
	}
	var folderB map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &folderB)
	idB := folderB["name"].(string)[len("folders/"):]

	moveBody := []byte(`{"destinationParent":"folders/` + idB + `"}`)
	req = httptest.NewRequest(http.MethodPost, "/v3/folders/"+idA+":move", bytes.NewReader(moveBody))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("move status=%d body=%s", rec.Code, rec.Body.String())
	}
	var moved map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &moved)
	if moved["parent"] != "folders/"+idB {
		t.Fatalf("moved parent = %#v", moved["parent"])
	}

	req = httptest.NewRequest(http.MethodGet, "/v3/folders/"+idA, nil)
	req.Header.Set("Authorization", auth)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get moved folder status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got["parent"] != "folders/"+idB {
		t.Fatalf("get parent = %#v", got["parent"])
	}
}

func TestAppEngineViaServer(t *testing.T) {
	srv, cfg := testServer(t)
	appID := cfg.ProjectID

	createApp := []byte(`{"id":"` + appID + `","locationId":"us-central"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/apps", bytes.NewReader(createApp))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create app status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/apps/"+appID, nil)
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get app status=%d body=%s", rec.Code, rec.Body.String())
	}

	verBody := []byte(`{"id":"v1","runtime":"nodejs20","envVariables":{"GREETING":"hi"}}`)
	req = httptest.NewRequest(http.MethodPost, "/v1/apps/"+appID+"/services/default/versions", bytes.NewReader(verBody))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create version status=%d body=%s", rec.Code, rec.Body.String())
	}
	var ver map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &ver)
	if ver["runtime"] != "nodejs20" {
		t.Fatalf("version = %#v", ver)
	}
	env, _ := ver["envVariables"].(map[string]any)
	if env["GREETING"] != "hi" {
		t.Fatalf("envVariables = %#v", env)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/apps/"+appID+"/services", nil)
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list services status=%d body=%s", rec.Code, rec.Body.String())
	}
	var svcList map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &svcList)
	services, _ := svcList["services"].([]any)
	if len(services) != 1 {
		t.Fatalf("services = %#v", svcList)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/apps/"+appID+"/services/default", nil)
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get service status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/apps/"+appID+"/services/default/versions", nil)
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list versions status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/apps/"+appID+"/services/default/versions/v1", nil)
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get version status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/v1/apps/"+appID+"/services/default/versions/v1", nil)
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete version status=%d body=%s", rec.Code, rec.Body.String())
	}
}
