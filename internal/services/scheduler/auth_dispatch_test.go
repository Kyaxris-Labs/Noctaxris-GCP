package scheduler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/compute"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudrun"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/scheduler"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// handlerRoundTrip routes outbound Scheduler fire into the in-process mux (no :4588 listener).
type handlerRoundTrip struct{ h http.Handler }

func (rt handlerRoundTrip) RoundTrip(req *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	rt.h.ServeHTTP(rec, req)
	return rec.Result(), nil
}

func TestSchedulerRunInvokeWithOIDCToken(t *testing.T) {
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
	saEmail := "sched-runner@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, root); err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureServiceAccount(project, saEmail, "scheduler runner"); err != nil {
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
	runSvc := &cloudrun.Service{
		Store:   st,
		Authz:   &authz.Evaluator{Policies: st},
		Invoker: compute.MockInvoker{},
	}
	runSvc.Mount(mux, principalFrom)

	schedSvc := &scheduler.Service{
		Store: st,
		Authz: &authz.Evaluator{Policies: st},
		HTTPClient: &http.Client{
			Transport: handlerRoundTrip{h: mux},
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
	schedSvc.Mount(mux, principalFrom)

	loc := cloudrun.DefaultLocation
	runBase := "/v2/projects/" + project + "/locations/" + loc + "/services"
	body := `{"template":{"containers":[{"image":"demo"}],"labResponseBody":"{\"from\":\"run\"}"}}`
	req := httptest.NewRequest(http.MethodPost, runBase+"?serviceId=sched-target", bytes.NewReader([]byte(body)))
	req.Header.Set("Authorization", "Bearer test-root-token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create run status=%d body=%s", rec.Code, rec.Body.String())
	}

	pol := `{"policy":{"bindings":[{"role":"roles/run.invoker","members":["serviceAccount:` + saEmail + `"]}],"etag":"ACAB"}}`
	req = httptest.NewRequest(http.MethodPost, runBase+"/sched-target:setIamPolicy", bytes.NewReader([]byte(pol)))
	req.Header.Set("Authorization", "Bearer test-root-token")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setIam status=%d body=%s", rec.Code, rec.Body.String())
	}

	invokeURL := "http://127.0.0.1:4588" + runBase + "/sched-target:invoke"
	jobBody := `{"schedule":"0 9 * * 1","httpTarget":{"uri":"` + invokeURL + `","httpMethod":"POST","oidcToken":{"serviceAccountEmail":"` + saEmail + `","audience":"` + invokeURL + `"},"body":"e30="}}`
	jobBase := "/v1/projects/" + project + "/locations/" + scheduler.DefaultLocation + "/jobs"
	req = httptest.NewRequest(http.MethodPost, jobBase+"?jobId=to-run", bytes.NewReader([]byte(jobBody)))
	req.Header.Set("Authorization", "Bearer test-root-token")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create job status=%d body=%s", rec.Code, rec.Body.String())
	}
	var job map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &job)
	ht, _ := job["httpTarget"].(map[string]any)
	oidc, _ := ht["oidcToken"].(map[string]any)
	if oidc["serviceAccountEmail"] != saEmail {
		t.Fatalf("oidcToken not echoed: %#v", ht)
	}

	req = httptest.NewRequest(http.MethodPost, jobBase+"/to-run:run", bytes.NewReader([]byte("{}")))
	req.Header.Set("Authorization", "Bearer test-root-token")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("job :run status=%d body=%s", rec.Code, rec.Body.String())
	}

	svc, ok, err := st.GetRunService("projects/" + project + "/locations/" + loc + "/services/sched-target")
	if err != nil || !ok {
		t.Fatalf("get run: ok=%v err=%v", ok, err)
	}
	if svc.LastInvokeJSON == "" {
		t.Fatal("expected LastInvokeJSON after authenticated Scheduler→Run fire (not 401)")
	}
	if !strings.Contains(svc.LastInvokeJSON, "sched-target") {
		t.Fatalf("unexpected LastInvokeJSON: %s", svc.LastInvokeJSON)
	}
}

func TestSchedulerOIDCPersistedOnGet(t *testing.T) {
	mux := mountScheduler(t, nil)
	base := "/v1/projects/noctaxris-gcp-local/locations/" + scheduler.DefaultLocation + "/jobs"
	body := `{"schedule":"0 9 * * 1","httpTarget":{"uri":"http://127.0.0.1:4588/_noctaxris-gcp/http-catcher/x","httpMethod":"POST","oidcToken":{"serviceAccountEmail":"sa@x","audience":"https://aud"}}}`
	req := httptest.NewRequest(http.MethodPost, base+"?jobId=oidc-keep", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var job map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &job)
	ht, _ := job["httpTarget"].(map[string]any)
	if _, ok := ht["oidcToken"]; !ok {
		t.Fatalf("oidcToken stripped: %#v", ht)
	}
}
