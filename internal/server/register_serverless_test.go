package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudrun"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudfunctions"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudtasks"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/scheduler"
)

func TestCloudRunCRUDAndInvokeViaServer(t *testing.T) {
	srv, cfg := testServer(t)
	loc := cloudrun.DefaultLocation
	base := "/v2/projects/" + cfg.ProjectID + "/locations/" + loc + "/services"
	body := `{"template":{"containers":[{"image":"us-docker.pkg.dev/demo/hello","env":[{"name":"GREETING","value":"hi"}]}],"labResponseBody":"{\"hello\":\"world\"}"}}`
	req := httptest.NewRequest(http.MethodPost, base+"?serviceId=demo", bytes.NewReader([]byte(body)))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var createOp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &createOp); err != nil {
		t.Fatal(err)
	}
	if createOp["done"] != true {
		t.Fatalf("create expected done Operation: %#v", createOp)
	}
	if _, ok := createOp["response"].(map[string]any); !ok {
		t.Fatalf("create missing response: %#v", createOp)
	}

	req = httptest.NewRequest(http.MethodGet, base+"/demo", nil)
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, base+"/demo/revisions", nil)
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("revisions status=%d body=%s", rec.Code, rec.Body.String())
	}
	var revResp struct {
		Revisions []map[string]any `json:"revisions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &revResp); err != nil {
		t.Fatal(err)
	}
	if len(revResp.Revisions) != 1 {
		t.Fatalf("revisions=%#v", revResp.Revisions)
	}

	req = httptest.NewRequest(http.MethodPost, base+"/demo:invoke", bytes.NewReader([]byte(`{"ping":1}`)))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("invoke status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"hello"`)) {
		t.Fatalf("invoke body=%s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, base+"/demo", nil)
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCloudFunctionsCRUDAndInvokeViaServer(t *testing.T) {
	srv, cfg := testServer(t)
	loc := cloudfunctions.DefaultLocation
	base := "/v2/projects/" + cfg.ProjectID + "/locations/" + loc + "/functions"
	body := `{"buildConfig":{"runtime":"nodejs20","entryPoint":"hello"},"labResponse":{"result":"ok"}}`
	req := httptest.NewRequest(http.MethodPost, base+"?functionId=fn1", bytes.NewReader([]byte(body)))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created["state"] != "ACTIVE" {
		t.Fatalf("state=%v", created["state"])
	}

	req = httptest.NewRequest(http.MethodPost, base+"/fn1:invoke", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("invoke status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"result"`)) {
		t.Fatalf("invoke body=%s", rec.Body.String())
	}
}

func TestSchedulerJobRunViaServer(t *testing.T) {
	srv, cfg := testServer(t)
	loc := scheduler.DefaultLocation
	base := "/v1/projects/" + cfg.ProjectID + "/locations/" + loc + "/jobs"
	body := `{"schedule":"0 9 * * 1","timeZone":"UTC","httpTarget":{"uri":"http://127.0.0.1:9/nope","httpMethod":"POST"}}`
	req := httptest.NewRequest(http.MethodPost, base+"?jobId=daily", bytes.NewReader([]byte(body)))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, base+"/daily:run", bytes.NewReader([]byte("{}")))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("run status=%d body=%s", rec.Code, rec.Body.String())
	}
	var job map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &job)
	if job["lastAttemptTime"] == "" && job["lastAttemptTime"] == nil {
		t.Fatalf("expected lastAttemptTime set: %#v", job)
	}
	lat, _ := job["lastAttemptTime"].(string)
	if lat == "" {
		t.Fatalf("expected lastAttemptTime set: %#v", job)
	}
}

func TestCloudTasksQueueTaskRunViaServer(t *testing.T) {
	srv, cfg := testServer(t)
	loc := cloudtasks.DefaultLocation
	qBase := "/v2/projects/" + cfg.ProjectID + "/locations/" + loc + "/queues"
	req := httptest.NewRequest(http.MethodPost, qBase+"?queueId=default", bytes.NewReader([]byte("{}")))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create queue status=%d body=%s", rec.Code, rec.Body.String())
	}

	taskBody := `{"task":{"httpRequest":{"url":"http://127.0.0.1:9/nope","httpMethod":"POST","oidcToken":{"serviceAccountEmail":"sa@x"},"body":"e30="}},"taskId":"t1"}`
	req = httptest.NewRequest(http.MethodPost, qBase+"/default/tasks", bytes.NewReader([]byte(taskBody)))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create task status=%d body=%s", rec.Code, rec.Body.String())
	}
	var task map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &task)
	hr, _ := task["httpRequest"].(map[string]any)
	if _, ok := hr["oidcToken"]; ok {
		t.Fatalf("oidcToken should be stripped: %#v", hr)
	}

	req = httptest.NewRequest(http.MethodPost, qBase+"/default/tasks/t1:run", bytes.NewReader([]byte("{}")))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("run status=%d body=%s", rec.Code, rec.Body.String())
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &task)
	if dc, _ := task["dispatchCount"].(float64); dc < 1 {
		t.Fatalf("dispatchCount=%v", task["dispatchCount"])
	}
}

func TestServerlessAuthzFailClosed(t *testing.T) {
	srv, cfg := testServer(t)
	loc := cloudrun.DefaultLocation
	path := "/v2/projects/" + cfg.ProjectID + "/locations/" + loc + "/services?serviceId=x"
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte("{}")))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCloudRunTrafficJobsIAMDeepen(t *testing.T) {
	srv, cfg := testServer(t)
	token := cfg.RootAccessToken
	loc := cloudrun.DefaultLocation
	base := "/v2/projects/" + cfg.ProjectID + "/locations/" + loc + "/services"
	body := `{"template":{"containers":[{"image":"demo"}]},"traffic":[{"type":"TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST","percent":100}]}`
	req := httptest.NewRequest(http.MethodPost, base+"?serviceId=deep", bytes.NewReader([]byte(body)))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var op map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &op)
	svc, _ := op["response"].(map[string]any)
	if svc == nil || svc["traffic"] == nil {
		t.Fatalf("missing traffic in operation response: %#v", op)
	}

	req = httptest.NewRequest(http.MethodPost, base+"/deep:invoke", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Lab-Trace", "trace-1")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("invoke status=%d", rec.Code)
	}

	pol := `{"policy":{"bindings":[{"role":"roles/run.invoker","members":["user:lab@example.com"]}],"etag":"ACAB"}}`
	req = httptest.NewRequest(http.MethodPost, base+"/deep:setIamPolicy", bytes.NewReader([]byte(pol)))
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setIam status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, base+"/deep:getIamPolicy", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte("run.invoker")) {
		t.Fatalf("getIam status=%d body=%s", rec.Code, rec.Body.String())
	}

	jobs := "/v2/projects/" + cfg.ProjectID + "/locations/" + loc + "/jobs"
	req = httptest.NewRequest(http.MethodPost, jobs+"?jobId=batch1", bytes.NewReader([]byte(`{"template":{"template":{"containers":[{"image":"job"}]}}}`)))
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create job status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, jobs+"/batch1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get job status=%d", rec.Code)
	}
}

func TestCloudFunctionsUploadUrlPatchIAM(t *testing.T) {
	srv, cfg := testServer(t)
	token := cfg.RootAccessToken
	loc := cloudfunctions.DefaultLocation
	base := "/v2/projects/" + cfg.ProjectID + "/locations/" + loc + "/functions"
	req := httptest.NewRequest(http.MethodPost, base+":generateUploadUrl", bytes.NewReader([]byte("{}")))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte("uploadUrl")) {
		t.Fatalf("uploadUrl status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, base+"?functionId=fn2", bytes.NewReader([]byte(`{"buildConfig":{"runtime":"nodejs20"},"labResponse":{"v":1}}`)))
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPatch, base+"/fn2", bytes.NewReader([]byte(`{"description":"patched","labResponse":{"v":2}}`)))
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !bytes.Contains(rec.Body.Bytes(), []byte("patched")) {
		t.Fatalf("patch status=%d body=%s", rec.Code, rec.Body.String())
	}
	pol := `{"policy":{"bindings":[{"role":"roles/cloudfunctions.invoker","members":["allUsers"]}],"etag":"ACAB"}}`
	req = httptest.NewRequest(http.MethodPost, base+"/fn2:setIamPolicy", bytes.NewReader([]byte(pol)))
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setIam status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSchedulerOIDCAndNextRun(t *testing.T) {
	srv, cfg := testServer(t)
	loc := scheduler.DefaultLocation
	base := "/v1/projects/" + cfg.ProjectID + "/locations/" + loc + "/jobs"
	body := `{"schedule":"0 9 * * 1","timeZone":"UTC","httpTarget":{"uri":"http://127.0.0.1:9/nope","httpMethod":"POST","oidcToken":{"serviceAccountEmail":"sa@x","audience":"https://example.com"}}}`
	req := httptest.NewRequest(http.MethodPost, base+"?jobId=oidc", bytes.NewReader([]byte(body)))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var job map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &job)
	ht, _ := job["httpTarget"].(map[string]any)
	if _, ok := ht["oidcToken"]; ok {
		t.Fatalf("oidcToken should be stripped: %#v", ht)
	}
	if job["oidcTokenAudience"] != "https://example.com" {
		t.Fatalf("audience=%v", job["oidcTokenAudience"])
	}
	if job["scheduleTime"] == nil || job["scheduleTime"] == "" {
		t.Fatalf("expected scheduleTime next-run: %#v", job)
	}

	req = httptest.NewRequest(http.MethodPost, base+"/oidc:pause", bytes.NewReader([]byte("{}")))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	_ = json.Unmarshal(rec.Body.Bytes(), &job)
	if job["state"] != "PAUSED" {
		t.Fatalf("pause state=%v", job["state"])
	}
	req = httptest.NewRequest(http.MethodPost, base+"/oidc:resume", bytes.NewReader([]byte("{}")))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	_ = json.Unmarshal(rec.Body.Bytes(), &job)
	if job["state"] != "ENABLED" {
		t.Fatalf("resume state=%v", job["state"])
	}
}

func TestCloudTasksRateLimitsRetryAppEngine(t *testing.T) {
	srv, cfg := testServer(t)
	token := cfg.RootAccessToken
	loc := cloudtasks.DefaultLocation
	qBase := "/v2/projects/" + cfg.ProjectID + "/locations/" + loc + "/queues"
	body := `{"rateLimits":{"maxDispatchesPerSecond":10},"retryConfig":{"maxAttempts":5},"appEngineRoutingOverride":{"service":"default"}}`
	req := httptest.NewRequest(http.MethodPost, qBase+"?queueId=limited", bytes.NewReader([]byte(body)))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create queue status=%d body=%s", rec.Code, rec.Body.String())
	}
	var q map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &q)
	if q["rateLimits"] == nil || q["retryConfig"] == nil {
		t.Fatalf("queue missing limits: %#v", q)
	}

	taskBody := `{"task":{"appEngineHttpRequest":{"httpMethod":"POST","relativeUri":"/task"},"httpRequest":{"url":"http://127.0.0.1:9/nope","httpMethod":"POST"}},"taskId":"ae1"}`
	req = httptest.NewRequest(http.MethodPost, qBase+"/limited/tasks", bytes.NewReader([]byte(taskBody)))
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create task status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, qBase+"/limited/tasks/ae1:run", bytes.NewReader([]byte("{}")))
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("run status=%d body=%s", rec.Code, rec.Body.String())
	}
	var task map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &task)
	if dc, _ := task["dispatchCount"].(float64); dc < 1 {
		t.Fatalf("dispatchCount=%v", task["dispatchCount"])
	}
	if _, ok := task["appEngineHttpRequest"]; !ok {
		t.Fatalf("missing appEngineHttpRequest: %#v", task)
	}
}

