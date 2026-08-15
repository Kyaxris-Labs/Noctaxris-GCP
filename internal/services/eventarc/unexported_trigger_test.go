package eventarc

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestUnexportedGetDeleteTrigger(t *testing.T) {
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
	project := "noctaxris-gcp-local"
	root := "root@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, root); err != nil {
		t.Fatal(err)
	}
	svc := &Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	p := authn.Principal{Email: root, IsRoot: true}
	loc := "us-central1"
	_, created, err := st.CreateEventarcTrigger(store.EventarcTrigger{
		ProjectID: project, Location: loc, TriggerID: "priv",
		FiltersJSON: `[]`, DestinationJSON: `{"httpEndpoint":{"uri":"http://127.0.0.1:4588/_noctaxris-gcp/http-catcher/x"}}`,
		TransportJSON: `{}`,
	})
	if err != nil || !created {
		t.Fatalf("create: created=%v err=%v", created, err)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/locations/"+loc+"/triggers/priv", nil)
	req.SetPathValue("project", project)
	req.SetPathValue("location", loc)
	req.SetPathValue("trigger", "priv")
	rec := httptest.NewRecorder()
	svc.getTrigger(rec, req, p)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/v1/projects/"+project+"/locations/"+loc+"/triggers/priv", nil)
	req.SetPathValue("project", project)
	req.SetPathValue("location", loc)
	req.SetPathValue("trigger", "priv")
	rec = httptest.NewRecorder()
	svc.deleteTrigger(rec, req, p)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/projects/"+project+"/locations/"+loc+"/triggers/priv", nil)
	req.SetPathValue("project", project)
	req.SetPathValue("location", loc)
	req.SetPathValue("trigger", "priv")
	rec = httptest.NewRecorder()
	svc.getTrigger(rec, req, p)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get missing: %d", rec.Code)
	}
}
