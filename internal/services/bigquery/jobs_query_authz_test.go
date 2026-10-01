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

func TestJobsQueryRequiresTableDataAndCreate(t *testing.T) {
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
	jobber := "jobuser@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, root); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateBQDataset(store.BQDataset{ProjectID: project, DatasetID: "secrets"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateBQTable(store.BQTable{
		ProjectID: project, DatasetID: "secrets", TableID: "t",
		SchemaJSON: `[{"name":"id","type":"STRING"}]`,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertBQRows(project, "secrets", "t", []map[string]any{{"id": "secret-row"}}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateCustomRole(project, "jobOnly", "Job only", "", "GA", []string{"bigquery.jobs.create", "bigquery.jobs.get"}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutIAMPolicyJSON("projects/"+project, authz.Policy{
		Etag: "ACAB",
		Bindings: []authz.Binding{{
			Role:    "projects/" + project + "/roles/jobOnly",
			Members: []string{"serviceAccount:" + jobber},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	svc := &bigquery.Service{Store: st, Authz: &authz.Evaluator{Policies: st, Roles: st}}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: jobber, IsRoot: false}, true
	})
	base := "/bigquery/v2/projects/" + project

	req := httptest.NewRequest(http.MethodPost, base+"/queries",
		bytes.NewReader([]byte(`{"query":"SELECT * FROM secrets.t"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("SELECT without getData: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, base+"/queries",
		bytes.NewReader([]byte(`{"query":"CREATE TABLE other.newtab (id STRING)"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("CREATE without tables.create: %d %s", rec.Code, rec.Body.String())
	}
}
