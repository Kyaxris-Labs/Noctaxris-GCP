package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/artifactregistry"
)

func TestArtifactRegistryRepoPackageVersionViaServer(t *testing.T) {
	srv, cfg := testServer(t)
	auth := "Bearer " + cfg.RootAccessToken
	project := cfg.ProjectID
	loc := artifactregistry.DefaultLocation
	base := "/v1/projects/" + project + "/locations/" + loc + "/repositories"

	req := httptest.NewRequest(http.MethodPost, base+"?repositoryId=docker-lab",
		bytes.NewReader([]byte(`{"format":"DOCKER","description":"lab"}`)))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create repo status=%d body=%s", rec.Code, rec.Body.String())
	}
	var repo map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &repo)
	wantRepo := "projects/" + project + "/locations/" + loc + "/repositories/docker-lab"
	if repo["name"] != wantRepo || repo["format"] != "DOCKER" {
		t.Fatalf("repo = %#v", repo)
	}

	pkgURL := base + "/docker-lab/packages?packageId=hello"
	req = httptest.NewRequest(http.MethodPost, pkgURL, bytes.NewReader([]byte(`{"displayName":"hello"}`)))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create package status=%d body=%s", rec.Code, rec.Body.String())
	}

	verURL := base + "/docker-lab/packages/hello/versions?versionId=sha256:abc"
	req = httptest.NewRequest(http.MethodPost, verURL, bytes.NewReader([]byte(`{"description":"v1"}`)))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create version status=%d body=%s", rec.Code, rec.Body.String())
	}
	var ver map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &ver)
	wantVer := wantRepo + "/packages/hello/versions/sha256:abc"
	if ver["name"] != wantVer {
		t.Fatalf("version = %#v", ver)
	}

	req = httptest.NewRequest(http.MethodGet, base+"/docker-lab/packages/hello/versions/sha256:abc", nil)
	req.Header.Set("Authorization", auth)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get version status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base+"/docker-lab/packages/hello/versions", nil)
	req.Header.Set("Authorization", auth)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list versions status=%d body=%s", rec.Code, rec.Body.String())
	}
	var listBody map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &listBody)
	versions, _ := listBody["versions"].([]any)
	if len(versions) != 1 {
		t.Fatalf("list versions = %#v", listBody)
	}
}

func TestCloudBuildWorkingThenSuccessAndProjectTriggers(t *testing.T) {
	srv, cfg := testServer(t)
	auth := "Bearer " + cfg.RootAccessToken
	project := cfg.ProjectID

	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/builds",
		bytes.NewReader([]byte(`{"steps":[{"name":"gcr.io/cloud-builders/docker","args":["build","."]}]}`)))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("createBuild status=%d body=%s", rec.Code, rec.Body.String())
	}
	var op map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &op)
	if op["done"] != false {
		t.Fatalf("createBuild op should not be done: %#v", op)
	}
	meta, _ := op["metadata"].(map[string]any)
	build, _ := meta["build"].(map[string]any)
	if build["status"] != "WORKING" {
		t.Fatalf("createBuild status=%#v", build["status"])
	}
	buildID, _ := build["id"].(string)
	if buildID == "" {
		t.Fatalf("missing build id: %#v", build)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/builds/"+buildID, nil)
	req.Header.Set("Authorization", auth)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("getBuild status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got["status"] != "SUCCESS" {
		t.Fatalf("getBuild status=%#v body=%s", got["status"], rec.Body.String())
	}
	if got["finishTime"] == nil || got["finishTime"] == "" {
		t.Fatalf("expected finishTime on SUCCESS: %#v", got)
	}

	trigBody := []byte(`{"id":"cb-trig-1","filename":"cloudbuild.yaml"}`)
	req = httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/triggers", bytes.NewReader(trigBody))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create trigger status=%d body=%s", rec.Code, rec.Body.String())
	}
	var trig map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &trig)
	if trig["id"] != "cb-trig-1" {
		t.Fatalf("trigger = %#v", trig)
	}
	if rn, _ := trig["resourceName"].(string); rn != "projects/"+project+"/triggers/cb-trig-1" {
		t.Fatalf("resourceName = %#v", trig["resourceName"])
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/triggers", nil)
	req.Header.Set("Authorization", auth)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list triggers status=%d body=%s", rec.Code, rec.Body.String())
	}
	var trigList map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &trigList)
	triggers, _ := trigList["triggers"].([]any)
	if len(triggers) != 1 {
		t.Fatalf("list triggers = %#v", trigList)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/triggers/cb-trig-1", nil)
	req.Header.Set("Authorization", auth)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get trigger status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCloudBuildAndEventarcTriggerPathSplit(t *testing.T) {
	srv, cfg := testServer(t)
	auth := "Bearer " + cfg.RootAccessToken
	project := cfg.ProjectID

	cbBody := []byte(`{"id":"cb-only","filename":"cloudbuild.yaml"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/triggers", bytes.NewReader(cbBody))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cloud build create trigger status=%d body=%s", rec.Code, rec.Body.String())
	}

	evtBody := []byte(`{
		"eventFilters":[{"attribute":"type","value":"google.cloud.pubsub.topic.v1.messagePublished"}],
		"destination":{"httpEndpoint":{"uri":"http://127.0.0.1:9/hook"}},
		"transport":{"pubsub":{"topic":"projects/` + project + `/topics/split-topic"}}
	}`)
	req = httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/locations/us-central1/triggers?triggerId=evt-only",
		bytes.NewReader(evtBody))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("eventarc create trigger status=%d body=%s", rec.Code, rec.Body.String())
	}
	var evt map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &evt)
	wantEvt := "projects/" + project + "/locations/us-central1/triggers/evt-only"
	if evt["name"] != wantEvt {
		t.Fatalf("eventarc trigger name = %#v", evt["name"])
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/triggers/cb-only", nil)
	req.Header.Set("Authorization", auth)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get cloud build trigger status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/locations/us-central1/triggers/evt-only", nil)
	req.Header.Set("Authorization", auth)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get eventarc trigger status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Regional .../locations/.../triggers stays Eventarc (Cloud Build is project-scoped only).
	req = httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/locations/us-central1/triggers?triggerId=evt-regional-2",
		bytes.NewReader(evtBody))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("regional create still Eventarc status=%d body=%s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &evt)
	wantRegional := "projects/" + project + "/locations/us-central1/triggers/evt-regional-2"
	if evt["name"] != wantRegional {
		t.Fatalf("expected Eventarc name %q, got %#v", wantRegional, evt)
	}
	if evt["eventFilters"] == nil {
		t.Fatalf("expected Eventarc eventFilters, got %#v", evt)
	}
	if _, hasCB := evt["resourceName"]; hasCB {
		t.Fatalf("regional path returned Cloud Build fields: %#v", evt)
	}
}

func TestArtifactRegistryRepoIAMViaServer(t *testing.T) {
	srv, cfg := testServer(t)
	auth := "Bearer " + cfg.RootAccessToken
	project := cfg.ProjectID
	loc := artifactregistry.DefaultLocation
	base := "/v1/projects/" + project + "/locations/" + loc + "/repositories"

	req := httptest.NewRequest(http.MethodPost, base+"?repositoryId=iam-lab",
		bytes.NewReader([]byte(`{"format":"DOCKER","description":"iam"}`)))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create repo status=%d body=%s", rec.Code, rec.Body.String())
	}

	pol := `{"policy":{"etag":"ACAB","bindings":[{"role":"roles/artifactregistry.reader","members":["allAuthenticatedUsers"]}]}}`
	req = httptest.NewRequest(http.MethodPost, base+"/iam-lab:setIamPolicy", bytes.NewReader([]byte(pol)))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setIamPolicy status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base+"/iam-lab:getIamPolicy", nil)
	req.Header.Set("Authorization", auth)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("getIamPolicy status=%d body=%s", rec.Code, rec.Body.String())
	}
	var gotPol map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &gotPol)
	bindings, _ := gotPol["bindings"].([]any)
	if len(bindings) != 1 {
		t.Fatalf("policy=%#v", gotPol)
	}
	binding, _ := bindings[0].(map[string]any)
	if binding["role"] != "roles/artifactregistry.reader" {
		t.Fatalf("binding=%#v", binding)
	}
}

func TestCloudBuildCancelRetryViaServer(t *testing.T) {
	srv, cfg := testServer(t)
	auth := "Bearer " + cfg.RootAccessToken
	project := cfg.ProjectID

	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/builds",
		bytes.NewReader([]byte(`{"steps":[{"name":"gcr.io/cloud-builders/docker"}]}`)))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("createBuild status=%d body=%s", rec.Code, rec.Body.String())
	}
	var op map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &op)
	meta, _ := op["metadata"].(map[string]any)
	build, _ := meta["build"].(map[string]any)
	buildID, _ := build["id"].(string)
	if buildID == "" {
		t.Fatalf("missing build id: %#v", build)
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/builds/"+buildID+":cancel",
		bytes.NewReader([]byte("{}")))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel status=%d body=%s", rec.Code, rec.Body.String())
	}
	var cancelled map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &cancelled)
	if cancelled["status"] != "CANCELLED" {
		t.Fatalf("cancel build=%#v", cancelled)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/builds/"+buildID, nil)
	req.Header.Set("Authorization", auth)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get cancelled status=%d body=%s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &cancelled)
	if cancelled["status"] != "CANCELLED" {
		t.Fatalf("get should stay CANCELLED: %#v", cancelled)
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/builds/"+buildID+":retry",
		bytes.NewReader([]byte("{}")))
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("retry status=%d body=%s", rec.Code, rec.Body.String())
	}
	var retryOp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &retryOp)
	retryMeta, _ := retryOp["metadata"].(map[string]any)
	retryBuild, _ := retryMeta["build"].(map[string]any)
	if retryBuild["status"] != "WORKING" {
		t.Fatalf("retry build=%#v", retryBuild)
	}
	if retryBuild["id"] == buildID {
		t.Fatal("retry should allocate a new build id")
	}
}
