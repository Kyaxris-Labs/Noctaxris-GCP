package cloudbuild_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/labtoken"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudbuild"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func setupTwoSACloudBuild(t *testing.T) (st *store.Store, project, saA, saB string, who *authn.Principal) {
	t.Helper()
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err = store.Open(filepath.Join(dir, "data"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	project = "noctaxris-gcp-local"
	root := "root@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, root); err != nil {
		t.Fatal(err)
	}
	saA = "sa-a@" + project + ".iam.gserviceaccount.com"
	saB = "sa-b@" + project + ".iam.gserviceaccount.com"
	computeSA := labtoken.DefaultComputeSAEmail(project)
	for _, email := range []string{saA, saB, computeSA} {
		if err := st.CreateServiceAccount(store.ServiceAccount{
			ProjectID: project, Email: email, UniqueID: strings.Split(email, "@")[0], DisplayName: email,
		}); err != nil {
			t.Fatal(err)
		}
	}
	perms := []string{
		"cloudbuild.builds.create", "cloudbuild.builds.get", "cloudbuild.builds.list",
		"iam.serviceAccounts.actAs",
	}
	if _, err := st.CreateCustomRole(project, "cbLab", "Cloud Build Lab", "", "GA", perms); err != nil {
		t.Fatal(err)
	}
	role := "projects/" + project + "/roles/cbLab"
	if err := st.PutIAMPolicyJSON("projects/"+project, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    role,
			Members: []string{"serviceAccount:" + saA, "serviceAccount:" + saB},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutIAMPolicyJSON("projects/"+project+"/serviceAccounts/"+computeSA, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    role,
			Members: []string{"serviceAccount:" + saA, "serviceAccount:" + saB},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	p := authn.Principal{Email: saA, IsRoot: false}
	who = &p
	return st, project, saA, saB, who
}

func TestListBuildsIAMSeesAllProjectBuilds(t *testing.T) {
	st, project, saA, saB, who := setupTwoSACloudBuild(t)
	mux := http.NewServeMux()
	svc := &cloudbuild.Service{
		Store: st, Authz: &authz.Evaluator{Policies: st, Roles: st},
		StepRunner: holdRunner{},
	}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return *who, true
	})

	*who = authn.Principal{Email: saA, IsRoot: false}
	privateBody := `{"steps":[{"name":"alpine:3.23","args":["true"]}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/builds", bytes.NewReader([]byte(privateBody)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create private status=%d body=%s", rec.Code, rec.Body.String())
	}
	var op map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &op)
	meta, _ := op["metadata"].(map[string]any)
	privBuild, _ := meta["build"].(map[string]any)
	privID, _ := privBuild["id"].(string)
	if privID == "" {
		t.Fatal("missing private build id")
	}

	*who = authn.Principal{Email: saB, IsRoot: false}
	req = httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/builds", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list as B status=%d body=%s", rec.Code, rec.Body.String())
	}
	var list map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	builds, _ := list["builds"].([]any)
	seen := map[string]bool{}
	for _, item := range builds {
		m, _ := item.(map[string]any)
		id, _ := m["id"].(string)
		seen[id] = true
	}
	if !seen[privID] {
		t.Fatal("real GCP: SA B with builds.list must see SA A builds in the same project")
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/builds/"+privID, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("real GCP: SA B with builds.get must get SA A build, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAvailableSecretsInjectIntoStepEnv(t *testing.T) {
	st, mux, svc := setupStepExec(t)
	project := "noctaxris-gcp-local"
	secretName := "projects/" + project + "/secrets/build-token"
	if _, _, err := st.CreateSecret(secretName, project); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddSecretVersion(secretName, []byte("super-secret-value")); err != nil {
		t.Fatal(err)
	}
	computeSA := labtoken.DefaultComputeSAEmail(project)
	if err := st.EnsureServiceAccount(project, computeSA, "compute"); err != nil {
		t.Fatal(err)
	}
	if err := st.PutIAMPolicyJSON(secretName, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "roles/secretmanager.secretAccessor",
			Members: []string{"serviceAccount:" + computeSA},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	var seen cloudbuild.BuildStep
	svc.StepRunner = &cloudbuild.EngineRunner{
		Store: st,
		Authz: &authz.Evaluator{Policies: st},
		ExecuteStep: func(_ context.Context, step cloudbuild.BuildStep) (string, error) {
			seen = step
			return "step-ok\n", nil
		},
	}
	body := `{
		"steps":[{"name":"alpine:3.23","secretEnv":["BUILD_TOKEN"]}],
		"availableSecrets":{"secretManager":[{"versionName":"` + secretName + `/versions/latest","env":"BUILD_TOKEN"}]}
	}`
	created := postBuild(t, mux, body)
	id, _ := created["id"].(string)
	got := getBuild(t, mux, id)
	if got["status"] != "SUCCESS" {
		t.Fatalf("status=%#v detail=%#v", got["status"], got["statusDetail"])
	}
	val, ok := envVal(seen.Env, "BUILD_TOKEN")
	if !ok || val != "super-secret-value" {
		t.Fatalf("BUILD_TOKEN env=%#v seen=%#v", val, seen.Env)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/builds/"+id+"/logs", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("logs status=%d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type=%q", ct)
	}
	logs := rec.Body.String()
	if !strings.Contains(logs, "=== Step 0 ===") || !strings.Contains(logs, "step-ok") {
		t.Fatalf("logs=%q", logs)
	}
	if strings.Contains(logs, "super-secret-value") {
		t.Fatal("secret value must not appear in build logs")
	}
	logURL, _ := got["logUrl"].(string)
	if !strings.HasSuffix(logURL, "/builds/"+id+"/logs") {
		t.Fatalf("logUrl=%q", logURL)
	}
}

func TestAvailableSecretsWithoutSecretEnvDoesNotInject(t *testing.T) {
	st, mux, svc := setupStepExec(t)
	project := "noctaxris-gcp-local"
	secretName := "projects/" + project + "/secrets/build-token"
	if _, _, err := st.CreateSecret(secretName, project); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddSecretVersion(secretName, []byte("should-not-inject")); err != nil {
		t.Fatal(err)
	}
	var seen cloudbuild.BuildStep
	svc.StepRunner = &cloudbuild.EngineRunner{
		Store: st,
		Authz: &authz.Evaluator{Policies: st},
		ExecuteStep: func(_ context.Context, step cloudbuild.BuildStep) (string, error) {
			seen = step
			return "ok\n", nil
		},
	}
	body := `{
		"steps":[{"name":"alpine:3.23","args":["true"]}],
		"availableSecrets":{"secretManager":[{"versionName":"` + secretName + `/versions/latest","env":"BUILD_TOKEN"}]}
	}`
	created := postBuild(t, mux, body)
	id, _ := created["id"].(string)
	got := getBuild(t, mux, id)
	if got["status"] != "SUCCESS" {
		t.Fatalf("status=%#v detail=%#v", got["status"], got["statusDetail"])
	}
	if _, ok := envVal(seen.Env, "BUILD_TOKEN"); ok {
		t.Fatal("real GCP: availableSecrets without step secretEnv must not inject env")
	}
}

func TestSecretEnvDeniedWithoutBuildSAAccessor(t *testing.T) {
	st, mux, svc := setupStepExec(t)
	project := "noctaxris-gcp-local"
	secretName := "projects/" + project + "/secrets/build-token"
	if _, _, err := st.CreateSecret(secretName, project); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddSecretVersion(secretName, []byte("locked")); err != nil {
		t.Fatal(err)
	}
	svc.StepRunner = &cloudbuild.EngineRunner{
		Store: st,
		Authz: &authz.Evaluator{Policies: st},
		ExecuteStep: func(_ context.Context, step cloudbuild.BuildStep) (string, error) {
			return "should-not-run\n", nil
		},
	}
	body := `{
		"steps":[{"name":"alpine:3.23","secretEnv":["BUILD_TOKEN"]}],
		"availableSecrets":{"secretManager":[{"versionName":"` + secretName + `/versions/latest","env":"BUILD_TOKEN"}]}
	}`
	created := postBuild(t, mux, body)
	id, _ := created["id"].(string)
	got := getBuild(t, mux, id)
	if got["status"] != "FAILURE" {
		t.Fatalf("want FAILURE without secretAccessor, got %#v detail=%#v", got["status"], got["statusDetail"])
	}
}
