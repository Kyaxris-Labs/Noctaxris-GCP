package scheduler_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/scheduler"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestSchedulerCreateWithOIDCRequiresActAs(t *testing.T) {
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
	editor := "editor@" + project + ".iam.gserviceaccount.com"
	targetSA := "privileged@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, root); err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureServiceAccount(project, editor, "editor"); err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureServiceAccount(project, targetSA, "target"); err != nil {
		t.Fatal(err)
	}
	pol := authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "roles/editor",
			Members: []string{"serviceAccount:" + editor},
		}},
		Etag: "ACAB",
	}
	if err := st.PutIAMPolicyJSON("projects/"+project, pol); err != nil {
		t.Fatal(err)
	}

	auth := &authn.Authenticator{
		RootServiceAccount: root,
		RootAccessToken:    "test-root-token",
		Tokens:             st,
	}
	tok := "editor-token"
	if err := st.PutAccessToken(authn.HashToken(tok), editor, time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	svc := &scheduler.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		p, err := auth.AuthenticateRequest(r)
		return p, err == nil
	})

	body := `{"schedule":"0 9 * * 1","httpTarget":{"uri":"http://127.0.0.1:4588/_noctaxris-gcp/http-catcher/x","httpMethod":"POST","oidcToken":{"serviceAccountEmail":"` + targetSA + `"}}}`
	base := "/v1/projects/" + project + "/locations/" + scheduler.DefaultLocation + "/jobs"
	req := httptest.NewRequest(http.MethodPost, base+"?jobId=no-actas", bytes.NewReader([]byte(body)))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("editor without actAs status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, base+"?jobId=root-ok", bytes.NewReader([]byte(body)))
	req.Header.Set("Authorization", "Bearer test-root-token")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("root create status=%d body=%s", rec.Code, rec.Body.String())
	}
}
