package cloudsql_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/compute"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudsql"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

type stubLabDaemon struct {
	err     error
	execErr error
	execs   [][]string
}

func (s *stubLabDaemon) Enabled() bool { return true }

func (s *stubLabDaemon) StartLabDaemon(context.Context, string, string, []string, int) (compute.LabDaemonResult, error) {
	return compute.LabDaemonResult{}, s.err
}

func (s *stubLabDaemon) RemoveLabDaemon(context.Context, string) error { return nil }

func (s *stubLabDaemon) ExecLabDaemon(_ context.Context, _ string, cmd []string) error {
	s.execs = append(s.execs, cmd)
	return s.execErr
}

func TestCloudSQLInstancesCRUD(t *testing.T) {
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
	base := "/sql/v1/projects/" + project + "/instances"
	body := `{"databaseVersion":"POSTGRES_16","region":"us-central1","settings":{"tier":"db-f1-micro"}}`
	req := httptest.NewRequest(http.MethodPost, base+"?instanceId=lab-pg", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var op map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &op)
	if op["kind"] != "sql#operation" || op["status"] != "DONE" || op["operationType"] != "CREATE" {
		t.Fatalf("create op=%#v", op)
	}
	if op["name"] == nil || op["name"] == "" {
		t.Fatalf("create op missing name: %#v", op)
	}

	req = httptest.NewRequest(http.MethodGet, base+"/lab-pg", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}
	var inst map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &inst)
	if inst["state"] != "RUNNABLE" {
		t.Fatalf("instance=%#v", inst)
	}
	if inst["databaseVersion"] != "POSTGRES_16" {
		t.Fatalf("databaseVersion=%v", inst["databaseVersion"])
	}
	host, _ := inst["host"].(string)
	if host == "" {
		t.Fatal("expected theatre host")
	}
	if int(inst["port"].(float64)) != 5432 {
		t.Fatalf("port=%v", inst["port"])
	}

	req = httptest.NewRequest(http.MethodGet, base, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var list map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	items, _ := list["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("list=%#v", list)
	}

	req = httptest.NewRequest(http.MethodDelete, base+"/lab-pg", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	var delOp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &delOp)
	if delOp["kind"] != "sql#operation" || delOp["status"] != "DONE" || delOp["operationType"] != "DELETE" {
		t.Fatalf("delete op=%#v", delOp)
	}
	delName, _ := delOp["name"].(string)
	if delName == "" {
		t.Fatalf("delete op missing name: %#v", delOp)
	}
	req = httptest.NewRequest(http.MethodGet, "/sql/v1/projects/"+project+"/operations/"+delName, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("poll delete op status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCloudSQLMySQLEngine(t *testing.T) {
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

	base := "/sql/v1/projects/noctaxris-gcp-local/instances"
	body := `{"databaseVersion":"MYSQL_8_0","region":"us-central1"}`
	req := httptest.NewRequest(http.MethodPost, base+"?instanceId=lab-mysql", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var op map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &op)
	if op["status"] != "DONE" {
		t.Fatalf("create op=%#v", op)
	}
	req = httptest.NewRequest(http.MethodGet, base+"/lab-mysql", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}
	var inst map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &inst)
	if int(inst["port"].(float64)) != 3306 {
		t.Fatalf("port=%v", inst["port"])
	}
}

func TestCloudSQLAuthzFailClosed(t *testing.T) {
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
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})

	req := httptest.NewRequest(http.MethodGet, "/sql/v1/projects/noctaxris-gcp-local/instances", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCloudSQLNilAuthzFailClosed(t *testing.T) {
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

	mux := http.NewServeMux()
	svc := &cloudsql.Service{Store: st, Authz: nil}
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "nobody@example.com", IsRoot: false}, true
	})

	req := httptest.NewRequest(http.MethodGet, "/sql/v1/projects/noctaxris-gcp-local/instances", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("nil Authz expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCloudSQLInvalidDatabaseVersion(t *testing.T) {
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

	base := "/sql/v1/projects/noctaxris-gcp-local/instances"
	req := httptest.NewRequest(http.MethodPost, base+"?instanceId=bad", bytes.NewReader([]byte(`{"databaseVersion":"ORACLE_19"}`)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCloudSQLNestedEngineFailClosed(t *testing.T) {
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

	startErr := errors.New("engine start refused")
	mux := http.NewServeMux()
	svc := &cloudsql.Service{
		Store:   st,
		Authz:   &authz.Evaluator{Policies: st},
		Compute: &stubLabDaemon{err: startErr},
	}
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	})

	base := "/sql/v1/projects/noctaxris-gcp-local/instances"
	body := `{"databaseVersion":"POSTGRES_16","region":"us-central1"}`

	t.Setenv(compute.EnvNestedEngineFailClosed, "")
	req := httptest.NewRequest(http.MethodPost, base+"?instanceId=soft-sql", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("soft create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var softOp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &softOp)
	if softOp["status"] != "DONE" {
		t.Fatalf("soft create op=%#v", softOp)
	}
	req = httptest.NewRequest(http.MethodGet, base+"/soft-sql", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("soft get status=%d body=%s", rec.Code, rec.Body.String())
	}
	var soft map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &soft)
	if soft["state"] != "RUNNABLE" {
		t.Fatalf("soft instance=%#v", soft)
	}

	t.Setenv(compute.EnvNestedEngineFailClosed, "1")
	req = httptest.NewRequest(http.MethodPost, base+"?instanceId=hard-sql", bytes.NewReader([]byte(body)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("fail-closed create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var errBody map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &errBody)
	detail, _ := errBody["error"].(map[string]any)
	if detail["status"] != "FAILED_PRECONDITION" {
		t.Fatalf("fail-closed body=%#v", errBody)
	}
	msg, _ := detail["message"].(string)
	if msg == "" || !containsAll(msg, "nested engine start failed", "engine start refused") {
		t.Fatalf("message=%q", msg)
	}
	_, ok, err := st.GetCloudSQLInstance(store.CloudSQLInstanceResourceName("noctaxris-gcp-local", "hard-sql"))
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("fail-closed create should roll back store row")
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !bytes.Contains([]byte(s), []byte(p)) {
			return false
		}
	}
	return true
}

func TestCloudSQLCreateRequiresServiceUsage(t *testing.T) {
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
	if err := st.SetServiceUsageState(project, "sqladmin.googleapis.com", "DISABLED"); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	svc := &cloudsql.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: root, IsRoot: true}, true
	})

	base := "/sql/v1/projects/" + project + "/instances"
	body := `{"databaseVersion":"POSTGRES_16","region":"us-central1"}`
	req := httptest.NewRequest(http.MethodPost, base+"?instanceId=gated", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("disabled create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var errBody map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &errBody)
	detail, _ := errBody["error"].(map[string]any)
	if detail["status"] != "FAILED_PRECONDITION" {
		t.Fatalf("disabled create = %#v", errBody)
	}

	if err := st.SetServiceUsageState(project, "sqladmin.googleapis.com", "ENABLED"); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodPost, base+"?instanceId=gated", bytes.NewReader([]byte(body)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("enabled create status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCloudSQLUsersAndDatabasesCRUD(t *testing.T) {
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

	mux := http.NewServeMux()
	svc := &cloudsql.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: root, IsRoot: true}, true
	})

	base := "/sql/v1/projects/" + project + "/instances"
	body := `{"databaseVersion":"POSTGRES_16","region":"us-central1"}`
	req := httptest.NewRequest(http.MethodPost, base+"?instanceId=lab-ud", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("instance create status=%d body=%s", rec.Code, rec.Body.String())
	}

	users := base + "/lab-ud/users"
	req = httptest.NewRequest(http.MethodPost, users, bytes.NewReader([]byte(`{"name":"appuser","password":"lab-pass"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("user create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var op map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &op)
	if op["kind"] != "sql#operation" || op["status"] != "DONE" {
		t.Fatalf("user create op=%#v", op)
	}

	req = httptest.NewRequest(http.MethodGet, users+"/appuser", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("user get status=%d body=%s", rec.Code, rec.Body.String())
	}
	var user map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &user)
	if user["name"] != "appuser" {
		t.Fatalf("user=%#v", user)
	}
	if _, ok := user["password"]; ok {
		t.Fatalf("password must not appear on get: %#v", user)
	}

	dbs := base + "/lab-ud/databases"
	req = httptest.NewRequest(http.MethodPost, dbs, bytes.NewReader([]byte(`{"name":"appdb"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("database create status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, dbs+"/appdb", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("database get status=%d body=%s", rec.Code, rec.Body.String())
	}
	var db map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &db)
	if db["name"] != "appdb" {
		t.Fatalf("database=%#v", db)
	}

	req = httptest.NewRequest(http.MethodDelete, dbs+"/appdb", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("database delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodDelete, users+"?name=appuser", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("user delete status=%d body=%s", rec.Code, rec.Body.String())
	}
}
