package cloudtasks_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudfunctions"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudtasks"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

type captureRoundTrip struct {
	h http.Handler

	mu            sync.Mutex
	invokeStatuses []int
}

func (rt *captureRoundTrip) RoundTrip(req *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	rt.h.ServeHTTP(rec, req)
	if strings.Contains(req.URL.Path, ":invoke") || strings.Contains(req.URL.RawPath, ":invoke") || strings.Contains(req.URL.String(), ":invoke") {
		rt.mu.Lock()
		rt.invokeStatuses = append(rt.invokeStatuses, rec.Code)
		rt.mu.Unlock()
	}
	return rec.Result(), nil
}

func TestTasksFunctionsInvokeWithOIDCToken(t *testing.T) {
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
	saEmail := "tasks-runner@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, root); err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureServiceAccount(project, saEmail, "tasks runner"); err != nil {
		t.Fatal(err)
	}

	auth := &authn.Authenticator{
		RootServiceAccount: root,
		RootAccessToken:    "test-root-token",
		Tokens:             st,
	}
	principalFrom := func(r *http.Request) (authn.Principal, bool) {
		p, err := auth.AuthenticateRequest(r)
		return p, err == nil
	}

	mux := http.NewServeMux()
	fnSvc := &cloudfunctions.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	fnSvc.Mount(mux, principalFrom)

	capt := &captureRoundTrip{h: mux}
	tasksSvc := &cloudtasks.Service{
		Store: st,
		Authz: &authz.Evaluator{Policies: st},
		HTTPClient: &http.Client{
			Transport: capt,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
	tasksSvc.Mount(mux, principalFrom)

	loc := cloudfunctions.DefaultLocation
	fnBase := "/v2/projects/" + project + "/locations/" + loc + "/functions"
	body := `{"labResponse":{"from":"fn"}}`
	req := httptest.NewRequest(http.MethodPost, fnBase+"?functionId=tasks-target", bytes.NewReader([]byte(body)))
	req.Header.Set("Authorization", "Bearer test-root-token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create function status=%d body=%s", rec.Code, rec.Body.String())
	}

	pol := `{"policy":{"bindings":[{"role":"roles/cloudfunctions.invoker","members":["serviceAccount:` + saEmail + `"]}],"etag":"ACAB"}}`
	req = httptest.NewRequest(http.MethodPost, fnBase+"/tasks-target:setIamPolicy", bytes.NewReader([]byte(pol)))
	req.Header.Set("Authorization", "Bearer test-root-token")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setIam status=%d body=%s", rec.Code, rec.Body.String())
	}

	qBase := "/v2/projects/" + project + "/locations/" + cloudtasks.DefaultLocation + "/queues"
	req = httptest.NewRequest(http.MethodPost, qBase+"?queueId=default", bytes.NewReader([]byte("{}")))
	req.Header.Set("Authorization", "Bearer test-root-token")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create queue status=%d body=%s", rec.Code, rec.Body.String())
	}

	invokeURL := "http://127.0.0.1:4588" + fnBase + "/tasks-target:invoke"
	taskBody := `{"taskId":"t-auth","task":{"scheduleTime":"2099-01-01T00:00:00Z","httpRequest":{"url":"` + invokeURL + `","httpMethod":"POST","oidcToken":{"serviceAccountEmail":"` + saEmail + `"},"body":"e30="}}}`
	req = httptest.NewRequest(http.MethodPost, qBase+"/default/tasks", bytes.NewReader([]byte(taskBody)))
	req.Header.Set("Authorization", "Bearer test-root-token")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create task status=%d body=%s", rec.Code, rec.Body.String())
	}
	var task map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &task)
	hr, _ := task["httpRequest"].(map[string]any)
	if _, ok := hr["oidcToken"]; !ok {
		t.Fatalf("oidcToken should persist: %#v", hr)
	}

	req = httptest.NewRequest(http.MethodPost, qBase+"/default/tasks/t-auth:run", bytes.NewReader([]byte("{}")))
	req.Header.Set("Authorization", "Bearer test-root-token")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("task :run status=%d body=%s", rec.Code, rec.Body.String())
	}

	capt.mu.Lock()
	statuses := append([]int(nil), capt.invokeStatuses...)
	capt.mu.Unlock()
	if len(statuses) == 0 {
		t.Fatal("expected Functions :invoke dispatch")
	}
	for _, code := range statuses {
		if code == http.StatusUnauthorized {
			t.Fatalf("Functions :invoke returned 401; statuses=%v", statuses)
		}
		if code != http.StatusOK {
			t.Fatalf("Functions :invoke status=%d; statuses=%v", code, statuses)
		}
	}
}
