package cloudsql_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudsql"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestCloudSQLOperationGetAndV1Beta4(t *testing.T) {
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

	body := `{"databaseVersion":"POSTGRES_16","region":"us-central1","settings":{"tier":"db-f1-micro"}}`
	req := httptest.NewRequest(http.MethodPost, "/sql/v1/projects/"+project+"/instances?instanceId=lab-op", bytes.NewReader([]byte(body)))
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
	opName, _ := op["name"].(string)
	if opName == "" {
		t.Fatalf("missing operation name: %#v", op)
	}

	req = httptest.NewRequest(http.MethodGet, "/sql/v1/projects/"+project+"/operations/"+opName, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get operation status=%d body=%s", rec.Code, rec.Body.String())
	}
	var polled map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &polled)
	if polled["kind"] != "sql#operation" || polled["status"] != "DONE" || polled["name"] != opName {
		t.Fatalf("polled=%#v", polled)
	}
	if polled["targetId"] != "lab-op" || polled["operationType"] != "CREATE" {
		t.Fatalf("polled fields=%#v", polled)
	}

	req = httptest.NewRequest(http.MethodGet, "/sql/v1/projects/"+project+"/instances/lab-op", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get instance status=%d body=%s", rec.Code, rec.Body.String())
	}
	var inst map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &inst)
	if inst["state"] != "RUNNABLE" {
		t.Fatalf("instance=%#v", inst)
	}

	// v1beta4 alias: create + Operations.get + instance get
	req = httptest.NewRequest(http.MethodPost, "/sql/v1beta4/projects/"+project+"/instances?instanceId=lab-beta", bytes.NewReader([]byte(body)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("v1beta4 create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var betaOp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &betaOp)
	betaName, _ := betaOp["name"].(string)
	if betaOp["status"] != "DONE" || betaName == "" {
		t.Fatalf("v1beta4 create op=%#v", betaOp)
	}
	req = httptest.NewRequest(http.MethodGet, "/sql/v1beta4/projects/"+project+"/operations/"+betaName, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("v1beta4 get op status=%d body=%s", rec.Code, rec.Body.String())
	}
	var betaPolled map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &betaPolled)
	if betaPolled["status"] != "DONE" {
		t.Fatalf("v1beta4 polled=%#v", betaPolled)
	}
	req = httptest.NewRequest(http.MethodGet, "/sql/v1beta4/projects/"+project+"/instances/lab-beta", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("v1beta4 get instance status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/sql/v1/projects/"+project+"/operations", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list operations status=%d body=%s", rec.Code, rec.Body.String())
	}
	var list map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if list["kind"] != "sql#operationsList" {
		t.Fatalf("list=%#v", list)
	}

	users := "/sql/v1/projects/" + project + "/instances/lab-op/users"
	req = httptest.NewRequest(http.MethodPost, users, bytes.NewReader([]byte(`{"name":"appuser","password":"lab-pass"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create user status=%d body=%s", rec.Code, rec.Body.String())
	}
	var userOp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &userOp)
	userOpName, _ := userOp["name"].(string)
	if userOp["operationType"] != "CREATE_USER" || userOpName == "" {
		t.Fatalf("user op=%#v", userOp)
	}
	req = httptest.NewRequest(http.MethodGet, "/sql/v1/projects/"+project+"/operations/"+userOpName, nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get user op status=%d body=%s", rec.Code, rec.Body.String())
	}
	var userPolled map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &userPolled)
	if userPolled["status"] != "DONE" || userPolled["operationType"] != "CREATE_USER" {
		t.Fatalf("user polled=%#v", userPolled)
	}
}

func TestCloudSQLOperationAuthzFailClosed(t *testing.T) {
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

	req := httptest.NewRequest(http.MethodGet, "/sql/v1/projects/noctaxris-gcp-local/operations/create-x", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}
