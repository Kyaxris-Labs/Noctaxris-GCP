package cloudtasks_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/httpegress"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudtasks"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func mountCloudTasks(t *testing.T, principal func(*http.Request) (authn.Principal, bool)) *http.ServeMux {
	t.Helper()
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "secrets", "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "data"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	const project = "noctaxris-gcp-local"
	root := "root@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, root); err != nil {
		t.Fatal(err)
	}
	if principal == nil {
		principal = func(*http.Request) (authn.Principal, bool) {
			return authn.Principal{Email: root, IsRoot: true}, true
		}
	}
	mux := http.NewServeMux()
	svc := &cloudtasks.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, principal)
	return mux
}

func TestCloudTasksQueueTaskRun(t *testing.T) {
	mux := mountCloudTasks(t, nil)
	loc := cloudtasks.DefaultLocation
	project := "noctaxris-gcp-local"
	qBase := "/v2/projects/" + project + "/locations/" + loc + "/queues"

	req := httptest.NewRequest(http.MethodPost, qBase+"?queueId=q1", bytes.NewReader([]byte(`{}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create queue status=%d body=%s", rec.Code, rec.Body.String())
	}

	catcher := "http://127.0.0.1:4588/_noctaxris-gcp/http-catcher/tasks-unit"
	taskBody := fmt.Sprintf(`{"taskId":"t1","task":{"httpRequest":{"url":"%s","httpMethod":"POST"}}}`, catcher)
	req = httptest.NewRequest(http.MethodPost, qBase+"/q1/tasks", bytes.NewReader([]byte(taskBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create task status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, qBase+"/q1/tasks/t1:run", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("run status=%d body=%s", rec.Code, rec.Body.String())
	}
	var task map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &task)
	if dc, _ := task["dispatchCount"].(float64); dc < 1 {
		t.Fatalf("dispatchCount=%v", task["dispatchCount"])
	}
}

func TestCloudTasksHTTPRequestSSRFFailClosed(t *testing.T) {
	t.Setenv(httpegress.EnvHTTPEgress, "")
	t.Setenv(httpegress.EnvHTTPAllowlist, "")

	mux := mountCloudTasks(t, nil)
	loc := cloudtasks.DefaultLocation
	project := "noctaxris-gcp-local"
	qBase := "/v2/projects/" + project + "/locations/" + loc + "/queues"

	req := httptest.NewRequest(http.MethodPost, qBase+"?queueId=q-ssrf", bytes.NewReader([]byte(`{}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create queue status=%d body=%s", rec.Code, rec.Body.String())
	}

	blocked := []string{
		`{"taskId":"evil-private","task":{"httpRequest":{"url":"http://10.0.0.1/internal","httpMethod":"POST"}}}`,
		`{"taskId":"evil-public","task":{"httpRequest":{"url":"https://example.com/hook","httpMethod":"POST"}}}`,
		`{"taskId":"evil-metadata","task":{"httpRequest":{"url":"http://169.254.169.254/latest","httpMethod":"POST"}}}`,
	}
	for i, body := range blocked {
		req = httptest.NewRequest(http.MethodPost, qBase+"/q-ssrf/tasks", bytes.NewReader([]byte(body)))
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("task %d: expected 400, got %d body=%s", i, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "http egress") {
			t.Fatalf("task %d: body=%s", i, rec.Body.String())
		}
	}
}

func TestCloudTasksAuthzDenyNonRoot(t *testing.T) {
	mux := mountCloudTasks(t, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})
	qBase := "/v2/projects/noctaxris-gcp-local/locations/" + cloudtasks.DefaultLocation + "/queues"
	req := httptest.NewRequest(http.MethodGet, qBase, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}
