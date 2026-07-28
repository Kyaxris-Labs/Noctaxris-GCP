package spanner_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/spanner"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestSpannerInstancesDatabasesExecuteSQL(t *testing.T) {
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
	svc := &spanner.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	})

	instBase := "/v1/projects/noctaxris-gcp-local/instances"
	body := `{"instanceId":"lab","instance":{"config":"projects/noctaxris-gcp-local/instanceConfigs/regional-us-central1","displayName":"Lab","nodeCount":1}}`
	req := httptest.NewRequest(http.MethodPost, instBase, bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create instance status=%d body=%s", rec.Code, rec.Body.String())
	}
	var inst map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &inst)
	if inst["state"] != "READY" {
		t.Fatalf("instance=%#v", inst)
	}

	dbBase := instBase + "/lab/databases"
	req = httptest.NewRequest(http.MethodPost, dbBase, bytes.NewReader([]byte(`{"createStatement":"CREATE DATABASE `+"`"+`app`+"`"+`"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create database status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, dbBase+"/app", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get database status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, dbBase+"/app/sessions", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create session status=%d body=%s", rec.Code, rec.Body.String())
	}
	var sess map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &sess)
	sessName, _ := sess["name"].(string)
	if sessName == "" {
		t.Fatal("missing session name")
	}
	parts := bytes.Split([]byte(sessName), []byte("/"))
	sessID := string(parts[len(parts)-1])

	req = httptest.NewRequest(http.MethodPost, dbBase+"/app/sessions/"+sessID+":executeSql", bytes.NewReader([]byte(`{"sql":"SELECT 1"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("executeSql status=%d body=%s", rec.Code, rec.Body.String())
	}
	var rs map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &rs)
	rows, _ := rs["rows"].([]any)
	if rows == nil {
		t.Fatalf("expected rows array: %#v", rs)
	}
	if len(rows) != 0 {
		t.Fatalf("theatre should return empty rows: %#v", rows)
	}

	req = httptest.NewRequest(http.MethodDelete, dbBase+"/app", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete database status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, instBase+"/lab", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete instance status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSpannerUpdateDDLBatchCreatePartitionQueryAndConfigs(t *testing.T) {
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
	svc := &spanner.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@noctaxris-gcp-local.iam.gserviceaccount.com", IsRoot: true}, true
	})

	instBase := "/v1/projects/noctaxris-gcp-local/instances"
	body := `{"instanceId":"lab","instance":{"config":"projects/noctaxris-gcp-local/instanceConfigs/regional-us-central1","displayName":"Lab","nodeCount":1}}`
	req := httptest.NewRequest(http.MethodPost, instBase, bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create instance status=%d body=%s", rec.Code, rec.Body.String())
	}

	dbBase := instBase + "/lab/databases"
	req = httptest.NewRequest(http.MethodPost, dbBase, bytes.NewReader([]byte(`{"createStatement":"CREATE DATABASE `+"`"+`app`+"`"+`"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create database status=%d body=%s", rec.Code, rec.Body.String())
	}

	ddl := `{"statements":["CREATE TABLE T (id INT64) PRIMARY KEY(id)"]}`
	req = httptest.NewRequest(http.MethodPatch, dbBase+"/app/ddl", bytes.NewReader([]byte(ddl)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("updateDdl status=%d body=%s", rec.Code, rec.Body.String())
	}
	var op map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &op)
	if op["done"] != true {
		t.Fatalf("op = %#v", op)
	}

	req = httptest.NewRequest(http.MethodGet, dbBase+"/app", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var db map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &db)
	stmts, _ := db["ddlStatements"].([]any)
	if len(stmts) != 1 {
		t.Fatalf("ddlStatements = %#v", db["ddlStatements"])
	}

	req = httptest.NewRequest(http.MethodPost, dbBase+"/app/sessions:batchCreate", bytes.NewReader([]byte(`{"sessionCount":2}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("batchCreate status=%d body=%s", rec.Code, rec.Body.String())
	}
	var batch map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &batch)
	sessions, _ := batch["session"].([]any)
	if len(sessions) != 2 {
		t.Fatalf("batch sessions = %#v", batch)
	}
	first, _ := sessions[0].(map[string]any)
	sessName, _ := first["name"].(string)
	parts := bytes.Split([]byte(sessName), []byte("/"))
	sessID := string(parts[len(parts)-1])

	req = httptest.NewRequest(http.MethodPost, dbBase+"/app/sessions/"+sessID+":read", bytes.NewReader([]byte(`{"table":"T"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("read status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, dbBase+"/app/sessions/"+sessID+":partitionQuery", bytes.NewReader([]byte(`{"sql":"SELECT 1"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("partitionQuery status=%d body=%s", rec.Code, rec.Body.String())
	}
	var part map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &part)
	partsList, _ := part["partitions"].([]any)
	if len(partsList) != 1 {
		t.Fatalf("partitions = %#v", part)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/projects/noctaxris-gcp-local/instanceConfigs", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list configs status=%d body=%s", rec.Code, rec.Body.String())
	}
	var configs map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &configs)
	cfgs, _ := configs["instanceConfigs"].([]any)
	if len(cfgs) != 1 {
		t.Fatalf("configs = %#v", configs)
	}
}

func TestSpannerFailClosedWithoutPrincipal(t *testing.T) {
	mux := http.NewServeMux()
	svc := &spanner.Service{}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) { return authn.Principal{}, false })
	req := httptest.NewRequest(http.MethodGet, "/v1/projects/p/instances", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}
}
