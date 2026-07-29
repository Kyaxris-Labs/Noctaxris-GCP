package cloudsql_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudsql"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestSQLUserAndDatabaseCRUD(t *testing.T) {
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
	svc := &cloudsql.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	})

	project := "noctaxris-gcp-local"
	instBase := "/sql/v1/projects/" + project + "/instances"
	body := `{"databaseVersion":"POSTGRES_16","region":"us-central1"}`
	req := httptest.NewRequest(http.MethodPost, instBase+"?instanceId=lab-pg", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create instance status=%d body=%s", rec.Code, rec.Body.String())
	}

	usersBase := instBase + "/lab-pg/users"
	req = httptest.NewRequest(http.MethodPost, usersBase, bytes.NewReader([]byte(`{"name":"appuser","password":"lab-pass"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create user status=%d body=%s", rec.Code, rec.Body.String())
	}
	var op map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &op)
	if op["kind"] != "sql#operation" || op["status"] != "DONE" || op["operationType"] != "CREATE_USER" {
		t.Fatalf("create user op=%#v", op)
	}

	req = httptest.NewRequest(http.MethodGet, usersBase+"/appuser", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get user status=%d body=%s", rec.Code, rec.Body.String())
	}
	var user map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &user)
	if user["name"] != "appuser" || user["password"] != nil {
		t.Fatalf("user=%#v", user)
	}

	req = httptest.NewRequest(http.MethodGet, usersBase, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list users status=%d body=%s", rec.Code, rec.Body.String())
	}
	var usersList map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &usersList)
	items, _ := usersList["items"].([]any)
	if usersList["kind"] != "sql#usersList" || len(items) != 1 {
		t.Fatalf("users list=%#v", usersList)
	}

	dbsBase := instBase + "/lab-pg/databases"
	req = httptest.NewRequest(http.MethodPost, dbsBase, bytes.NewReader([]byte(`{"name":"appdb"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create database status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, dbsBase+"/appdb", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get database status=%d body=%s", rec.Code, rec.Body.String())
	}
	var db map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &db)
	if db["name"] != "appdb" || db["charset"] != "UTF8" {
		t.Fatalf("database=%#v", db)
	}

	req = httptest.NewRequest(http.MethodGet, dbsBase, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list databases status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, usersBase+"?name=appuser", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete user status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, dbsBase+"/appdb", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete database status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSQLUserAuthzFailClosed(t *testing.T) {
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
	_, err = st.CreateCloudSQLInstance(store.CloudSQLInstance{
		Name: store.CloudSQLInstanceResourceName("noctaxris-gcp-local", "lab-pg"),
		ProjectID: "noctaxris-gcp-local", InstanceID: "lab-pg", Region: "us-central1",
		DatabaseVersion: "POSTGRES_16",
	})
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	svc := &cloudsql.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})

	req := httptest.NewRequest(http.MethodGet, "/sql/v1/projects/noctaxris-gcp-local/instances/lab-pg/users", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSQLUserNestedExecSoft(t *testing.T) {
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

	name := store.CloudSQLInstanceResourceName("noctaxris-gcp-local", "lab-pg")
	if _, err := st.CreateCloudSQLInstance(store.CloudSQLInstance{
		Name: name, ProjectID: "noctaxris-gcp-local", InstanceID: "lab-pg", Region: "us-central1",
		DatabaseVersion: "POSTGRES_16",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateCloudSQLInstanceNested(name, "noctaxris-gcp-sql-lab-pg", 5432, "cid-sql-1"); err != nil {
		t.Fatal(err)
	}

	tracker := &stubLabDaemon{execErr: errors.New("exec refused")}
	mux := http.NewServeMux()
	svc := &cloudsql.Service{
		Store:   st,
		Authz:   &authz.Evaluator{Policies: st},
		Compute: tracker,
	}
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	})

	instBase := "/sql/v1/projects/noctaxris-gcp-local/instances"
	req := httptest.NewRequest(http.MethodPost, instBase+"/lab-pg/users",
		bytes.NewReader([]byte(`{"name":"appuser","password":"lab-pass"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create user soft status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(tracker.execs) != 1 {
		t.Fatalf("expected one nested exec, got %#v", tracker.execs)
	}
	cmd := strings.Join(tracker.execs[0], " ")
	if !strings.Contains(cmd, "CREATE USER") || !strings.Contains(cmd, "psql") {
		t.Fatalf("exec cmd=%q", cmd)
	}

	req = httptest.NewRequest(http.MethodPost, instBase+"/lab-pg/databases",
		bytes.NewReader([]byte(`{"name":"appdb"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create database soft status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(tracker.execs) != 2 {
		t.Fatalf("expected two nested execs, got %#v", tracker.execs)
	}
}
