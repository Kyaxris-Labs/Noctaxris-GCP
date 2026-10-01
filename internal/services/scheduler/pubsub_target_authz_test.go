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

func TestSchedulerPubsubTargetRequiresTopicsPublish(t *testing.T) {
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
	sched := "sched@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, root); err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureServiceAccount(project, sched, "scheduler"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateCustomRole(project, "schedOnly", "Scheduler", "", "GA", []string{
		"cloudscheduler.jobs.create", "cloudscheduler.jobs.run",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutIAMPolicyJSON("projects/"+project, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "projects/" + project + "/roles/schedOnly",
			Members: []string{"serviceAccount:" + sched},
		}},
		Etag: "ACAB",
	}); err != nil {
		t.Fatal(err)
	}

	topic := "projects/" + project + "/topics/sensitive"
	if _, ok, err := st.CreateTopic(topic, project); err != nil || !ok {
		t.Fatalf("topic: ok=%v err=%v", ok, err)
	}

	tok := "sched-token"
	if err := st.PutAccessToken(authn.HashToken(tok), sched, time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	auth := &authn.Authenticator{RootServiceAccount: root, RootAccessToken: "root-tok", Tokens: st}
	mux := http.NewServeMux()
	svc := &scheduler.Service{Store: st, Authz: &authz.Evaluator{Policies: st, Roles: st}}
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		p, err := auth.AuthenticateRequest(r)
		return p, err == nil
	})

	body := `{"schedule":"* * * * *","pubsubTarget":{"topicName":"` + topic + `","data":"aGk="}}`
	base := "/v1/projects/" + project + "/locations/" + scheduler.DefaultLocation + "/jobs"
	req := httptest.NewRequest(http.MethodPost, base+"?jobId=no-publish", bytes.NewReader([]byte(body)))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("scheduler without topics.publish status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, base+"?jobId=root-ok", bytes.NewReader([]byte(body)))
	req.Header.Set("Authorization", "Bearer root-tok")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("root create status=%d body=%s", rec.Code, rec.Body.String())
	}
}
