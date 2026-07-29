package scheduler_test

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
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/scheduler"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func mountScheduler(t *testing.T, principal func(*http.Request) (authn.Principal, bool)) *http.ServeMux {
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
	svc := &scheduler.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, principal)
	return mux
}

func TestSchedulerJobCreateRunHappyPath(t *testing.T) {
	store.ClearHTTPCatcher()
	t.Cleanup(store.ClearHTTPCatcher)

	mux := mountScheduler(t, nil)
	loc := scheduler.DefaultLocation
	project := "noctaxris-gcp-local"
	base := "/v1/projects/" + project + "/locations/" + loc + "/jobs"
	catcher := "http://127.0.0.1:4588/_noctaxris-gcp/http-catcher/sched-unit"
	body := `{"schedule":"0 9 * * 1","httpTarget":{"uri":"` + catcher + `","httpMethod":"POST","body":"c2NoZWQtcGF5bG9hZA=="}}`
	req := httptest.NewRequest(http.MethodPost, base+"?jobId=daily", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, base+"/daily:run", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("run status=%d body=%s", rec.Code, rec.Body.String())
	}
	var job map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &job)
	if job["lastAttemptTime"] == nil || job["lastAttemptTime"] == "" {
		t.Fatalf("expected lastAttemptTime: %#v", job)
	}
	caught := store.ListHTTPCatcher()
	if len(caught) != 1 || caught[0] != "sched-payload" {
		t.Fatalf("catcher deliveries = %#v", caught)
	}
}

func TestSchedulerHTTPTargetSSRFFailClosed(t *testing.T) {
	t.Setenv(httpegress.EnvHTTPEgress, "")
	t.Setenv(httpegress.EnvHTTPAllowlist, "")

	mux := mountScheduler(t, nil)
	base := "/v1/projects/noctaxris-gcp-local/locations/" + scheduler.DefaultLocation + "/jobs"
	blocked := []string{
		`{"schedule":"* * * * *","httpTarget":{"uri":"http://169.254.169.254/latest"}}`,
		`{"schedule":"* * * * *","httpTarget":{"uri":"https://example.com/hook"}}`,
	}
	for i, body := range blocked {
		req := httptest.NewRequest(http.MethodPost, base+"?jobId="+fmt.Sprintf("bad%d", i), bytes.NewReader([]byte(body)))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("job %d: expected 400, got %d body=%s", i, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "http egress") {
			t.Fatalf("job %d: body=%s", i, rec.Body.String())
		}
	}
}

func TestSchedulerAuthzDenyNonRoot(t *testing.T) {
	mux := mountScheduler(t, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})
	base := "/v1/projects/noctaxris-gcp-local/locations/" + scheduler.DefaultLocation + "/jobs"
	req := httptest.NewRequest(http.MethodGet, base, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}
