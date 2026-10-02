package pubsub_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/pubsub"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestPubSubPushOIDCRequiresActAs(t *testing.T) {
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "master.key"))
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
	targetSA := "push-target@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, root); err != nil {
		t.Fatal(err)
	}
	for _, email := range []string{editor, targetSA} {
		if err := st.EnsureServiceAccount(project, email, "lab"); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.PutIAMPolicyJSON("projects/"+project, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "roles/editor",
			Members: []string{"serviceAccount:" + editor},
		}},
		Etag: "ACAB",
	}); err != nil {
		t.Fatal(err)
	}

	topic := "projects/" + project + "/topics/oidc-actas"
	if _, ok, err := st.CreateTopic(topic, project); err != nil || !ok {
		t.Fatalf("topic: ok=%v err=%v", ok, err)
	}

	tok := "editor-token"
	if err := st.PutAccessToken(authn.HashToken(tok), editor, time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	auth := &authn.Authenticator{RootServiceAccount: root, RootAccessToken: "root-tok", Tokens: st}
	mux := http.NewServeMux()
	svc := &pubsub.Service{Store: st, Authz: &authz.Evaluator{Policies: st, Roles: st}}
	svc.RegisterREST(mux, func(r *http.Request) (authn.Principal, bool) {
		p, err := auth.AuthenticateRequest(r)
		return p, err == nil
	})

	catcher := "http://127.0.0.1:4588/_noctaxris-gcp/http-catcher/pubsub-actas"
	body := `{"topic":"` + topic + `","pushConfig":{"pushEndpoint":"` + catcher + `","oidcToken":{"serviceAccountEmail":"` + targetSA + `"}}}`
	req := httptest.NewRequest(http.MethodPut, "/v1/projects/"+project+"/subscriptions/no-actas", bytes.NewReader([]byte(body)))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("editor without actAs status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPut, "/v1/projects/"+project+"/subscriptions/root-ok", bytes.NewReader([]byte(body)))
	req.Header.Set("Authorization", "Bearer root-tok")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("root create status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPubSubCreateRequiresTopicAttach(t *testing.T) {
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "data"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	victim := "victim-proj"
	attacker := "attacker-proj"
	rootVictim := "root@" + victim + ".iam.gserviceaccount.com"
	caller := "attacker@" + attacker + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(victim, rootVictim); err != nil {
		t.Fatal(err)
	}
	if _, created, err := st.CreateProject(store.Project{ID: attacker, DisplayName: "Attacker"}); err != nil || !created {
		t.Fatalf("create project: created=%v err=%v", created, err)
	}
	if err := st.EnsureServiceAccount(attacker, caller, "attacker"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateCustomRole(attacker, "subCreate", "SubCreate", "", "GA", []string{
		"pubsub.subscriptions.create", "pubsub.subscriptions.consume",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutIAMPolicyJSON("projects/"+attacker, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "projects/" + attacker + "/roles/subCreate",
			Members: []string{"serviceAccount:" + caller},
		}},
		Etag: "ACAB",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetServiceUsageState(attacker, "pubsub.googleapis.com", "ENABLED"); err != nil {
		t.Fatal(err)
	}

	topic := "projects/" + victim + "/topics/secrets"
	if _, ok, err := st.CreateTopic(topic, victim); err != nil || !ok {
		t.Fatalf("topic: ok=%v err=%v", ok, err)
	}

	mux := http.NewServeMux()
	svc := &pubsub.Service{Store: st, Authz: &authz.Evaluator{Policies: st, Roles: st}}
	svc.RegisterREST(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: caller, IsRoot: false}, true
	})

	body := `{"topic":"` + topic + `"}`
	req := httptest.NewRequest(http.MethodPut, "/v1/projects/"+attacker+"/subscriptions/drain", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-project attach without permission status=%d body=%s", rec.Code, rec.Body.String())
	}
}
