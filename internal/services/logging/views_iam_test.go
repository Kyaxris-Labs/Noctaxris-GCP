package logging_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/logging"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestLoggingSinkDisabledPersistAndHonor(t *testing.T) {
	mux := setupLogging(t)
	create := `{"destination":"storage.googleapis.com/off","filter":"severity=ERROR","disabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/v2/projects/noctaxris-gcp-local/sinks?sinkId=paused", bytes.NewReader([]byte(create)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var sink map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &sink); err != nil {
		t.Fatal(err)
	}
	if sink["disabled"] != true {
		t.Fatalf("create sink %#v", sink)
	}
	req = httptest.NewRequest(http.MethodPatch, "/v2/projects/noctaxris-gcp-local/sinks/paused",
		bytes.NewReader([]byte(`{"disabled":false}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &sink); err != nil {
		t.Fatal(err)
	}
	if sink["disabled"] != false {
		t.Fatalf("patched sink %#v", sink)
	}
}

func TestLoggingViewScopedGetDeny(t *testing.T) {
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
	if err := st.EnsureRoot(project, "root@"+project+".iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureDefaultLogRouting(project); err != nil {
		t.Fatal(err)
	}
	va, created, err := st.CreateLogView(store.LogView{
		ProjectID: project, Location: "global", BucketID: "_Default", ViewID: "v-a",
	})
	if err != nil || !created {
		t.Fatalf("view a created=%v err=%v", created, err)
	}
	vb, created, err := st.CreateLogView(store.LogView{
		ProjectID: project, Location: "global", BucketID: "_Default", ViewID: "v-b",
	})
	if err != nil || !created {
		t.Fatalf("view b created=%v err=%v", created, err)
	}
	reader := "view-reader@" + project + ".iam.gserviceaccount.com"
	if _, err := st.CreateCustomRole(project, "viewListOnly", "View list", "", "GA", []string{"logging.views.list"}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutIAMPolicyJSON("projects/"+project, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "projects/" + project + "/roles/viewListOnly",
			Members: []string{"serviceAccount:" + reader},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutIAMPolicyJSON(va.Name, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "roles/logging.viewAccessor",
			Members: []string{"serviceAccount:" + reader},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	svc := &logging.Service{Store: st, Authz: &authz.Evaluator{Policies: st, Roles: st}}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: reader, IsRoot: false}, true
	})

	req := httptest.NewRequest(http.MethodGet, "/v2/"+va.Name, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get v-a status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v2/"+vb.Name, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("get v-b status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v2/projects/"+project+"/locations/global/buckets/_Default/views", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list views status=%d body=%s", rec.Code, rec.Body.String())
	}
	var listed struct {
		Views []map[string]any `json:"views"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed.Views) < 2 {
		t.Fatalf("list views %#v", listed.Views)
	}
}
