package cloudtasks_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudtasks"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestCloudTasksQueueTaskCRUDLifecycle(t *testing.T) {
	store.ClearHTTPCatcher()
	t.Cleanup(store.ClearHTTPCatcher)

	mux := mountCloudTasks(t, nil)
	loc := cloudtasks.DefaultLocation
	project := "noctaxris-gcp-local"
	qBase := "/v2/projects/" + project + "/locations/" + loc + "/queues"

	req := httptest.NewRequest(http.MethodPost, qBase+"?queueId=crud-q", bytes.NewReader([]byte(`{"rateLimits":{"maxDispatchesPerSecond":5}}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create queue: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, qBase, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list queues: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, qBase+"/crud-q", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get queue: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPatch, qBase+"/crud-q", bytes.NewReader([]byte(`{"rateLimits":{"maxDispatchesPerSecond":10}}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch queue: %d %s", rec.Code, rec.Body.String())
	}

	catcher := "http://127.0.0.1:4588/_noctaxris-gcp/http-catcher/tasks-crud"
	taskBody := fmt.Sprintf(`{"taskId":"crud-t","task":{"httpRequest":{"url":"%s","httpMethod":"POST","body":"YQ=="}}}`, catcher)
	req = httptest.NewRequest(http.MethodPost, qBase+"/crud-q/tasks", bytes.NewReader([]byte(taskBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create task: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, qBase+"/crud-q/tasks", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list tasks: %d %s", rec.Code, rec.Body.String())
	}
	var list map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &list)

	req = httptest.NewRequest(http.MethodGet, qBase+"/crud-q/tasks/crud-t", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	// Task may already have been dispatched/deleted on create; accept 200 or 404.
	if rec.Code != http.StatusOK && rec.Code != http.StatusNotFound {
		t.Fatalf("get task: %d %s", rec.Code, rec.Body.String())
	}

	if rec.Code == http.StatusOK {
		req = httptest.NewRequest(http.MethodDelete, qBase+"/crud-q/tasks/crud-t", nil)
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("delete task: %d %s", rec.Code, rec.Body.String())
		}
	}

	req = httptest.NewRequest(http.MethodDelete, qBase+"/crud-q", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete queue: %d %s", rec.Code, rec.Body.String())
	}
}
