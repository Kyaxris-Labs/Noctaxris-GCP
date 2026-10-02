package bigquery_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/bigquery"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestDatasetResourceIAMAllowsGet(t *testing.T) {
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
	if err := st.EnsureRoot(project, root); err != nil {
		t.Fatal(err)
	}
	reader := "reader@" + project + ".iam.gserviceaccount.com"
	if err := st.CreateServiceAccount(store.ServiceAccount{
		ProjectID: project, Email: reader, UniqueID: "reader", DisplayName: "reader",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateBQDataset(store.BQDataset{ProjectID: project, DatasetID: "ds1"}); err != nil {
		t.Fatal(err)
	}
	dsRes := "projects/" + project + "/datasets/ds1"
	if err := st.PutIAMPolicyJSON(dsRes, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "roles/bigquery.dataViewer",
			Members: []string{"serviceAccount:" + reader},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	(&bigquery.Service{Store: st, Authz: &authz.Evaluator{Policies: st, Roles: st}}).Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: reader, IsRoot: false}, true
	})
	req := httptest.NewRequest(http.MethodGet, "/bigquery/v2/projects/"+project+"/datasets/ds1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("dataset IAM get status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/bigquery/v2/projects/"+project+"/datasets", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("project list without project grant status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestQueryHonorsDatasetIAM(t *testing.T) {
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
	if err := st.EnsureRoot(project, root); err != nil {
		t.Fatal(err)
	}
	caller := "caller@" + project + ".iam.gserviceaccount.com"
	if err := st.CreateServiceAccount(store.ServiceAccount{
		ProjectID: project, Email: caller, UniqueID: "caller", DisplayName: "caller",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateBQDataset(store.BQDataset{ProjectID: project, DatasetID: "ds1"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateBQTable(store.BQTable{
		ProjectID: project, DatasetID: "ds1", TableID: "t1",
		SchemaJSON: `[{"name":"id","type":"STRING"}]`,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertBQRows(project, "ds1", "t1", []map[string]any{{"id": "1"}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.PutIAMPolicyJSON("projects/"+project, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "roles/bigquery.jobUser",
			Members: []string{"serviceAccount:" + caller},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	dsRes := "projects/" + project + "/datasets/ds1"
	if err := st.PutIAMPolicyJSON(dsRes, authz.Policy{
		Bindings: []authz.Binding{{
			Role:    "roles/bigquery.dataViewer",
			Members: []string{"serviceAccount:" + caller},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	(&bigquery.Service{Store: st, Authz: &authz.Evaluator{Policies: st, Roles: st}}).Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: caller, IsRoot: false}, true
	})
	req := httptest.NewRequest(http.MethodPost, "/bigquery/v2/projects/"+project+"/queries",
		bytes.NewReader([]byte(`{"query":"SELECT id FROM ds1.t1"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("dataset IAM query status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/bigquery/v2/projects/"+project+"/jobs/lab-missing", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("jobUser alone must not jobs.get status=%d body=%s", rec.Code, rec.Body.String())
	}
}
