package bigquery_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/bigquery"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func testBigQueryMux(t *testing.T) (*http.ServeMux, *store.Store) {
	t.Helper()
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
	if err := st.EnsureRoot("noctaxris-gcp-local", "root@noctaxris-gcp-local.iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	svc := &bigquery.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	})
	return mux, st
}

func TestBigQueryDatasetsTablesCRUDInsertAndQuery(t *testing.T) {
	mux, _ := testBigQueryMux(t)
	project := "noctaxris-gcp-local"
	dsBase := "/bigquery/v2/projects/" + project + "/datasets"

	req := httptest.NewRequest(http.MethodPost, dsBase,
		bytes.NewReader([]byte(`{"datasetReference":{"datasetId":"lab"},"location":"US"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create dataset status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, dsBase+"/lab", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get dataset status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, dsBase, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list datasets status=%d body=%s", rec.Code, rec.Body.String())
	}
	var dsList map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &dsList)
	datasets, _ := dsList["datasets"].([]any)
	if len(datasets) != 1 {
		t.Fatalf("datasets=%#v", dsList)
	}

	tblBase := dsBase + "/lab/tables"
	createTbl := `{"tableReference":{"tableId":"events"},"schema":{"fields":[{"name":"k","type":"STRING","mode":"REQUIRED"}]}}`
	req = httptest.NewRequest(http.MethodPost, tblBase, bytes.NewReader([]byte(createTbl)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create table status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, tblBase+"/events", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get table status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, tblBase, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list tables status=%d body=%s", rec.Code, rec.Body.String())
	}

	insert := `{"rows":[{"insertId":"1","json":{"k":"alpha"}},{"insertId":"2","json":{"k":"beta"}}]}`
	req = httptest.NewRequest(http.MethodPost, tblBase+"/events/insertAll", bytes.NewReader([]byte(insert)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("insertAll status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, tblBase+"/events/data", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("tabledata.list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var dataBody map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &dataBody)
	rows, _ := dataBody["rows"].([]any)
	if len(rows) != 2 {
		t.Fatalf("tabledata.list rows=%#v", dataBody)
	}

	query := `{"query":"SELECT k FROM lab.events WHERE k = 'alpha'"}`
	req = httptest.NewRequest(http.MethodPost, "/bigquery/v2/projects/"+project+"/queries", bytes.NewReader([]byte(query)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("jobs.query status=%d body=%s", rec.Code, rec.Body.String())
	}
	var qr map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &qr)
	if qr["totalRows"] != "1" {
		t.Fatalf("query=%#v", qr)
	}

	req = httptest.NewRequest(http.MethodDelete, tblBase+"/events", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete table status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, dsBase+"/lab", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete dataset status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestBigQueryAuthzFailClosed(t *testing.T) {
	mux := http.NewServeMux()
	svc := &bigquery.Service{}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) { return authn.Principal{}, false })
	req := httptest.NewRequest(http.MethodGet, "/bigquery/v2/projects/p/datasets", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestBigQueryAuthzDenyNonRootWithoutBinding(t *testing.T) {
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
	if err := st.EnsureRoot("noctaxris-gcp-local", "root@noctaxris-gcp-local.iam.gserviceaccount.com"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	svc := &bigquery.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})
	req := httptest.NewRequest(http.MethodGet, "/bigquery/v2/projects/noctaxris-gcp-local/datasets", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}
