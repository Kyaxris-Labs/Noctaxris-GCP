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

func TestExpandComputeAuthzFailClosed(t *testing.T) {
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
