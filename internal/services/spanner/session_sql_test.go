package spanner_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/spanner"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestSpannerSessionExecuteSqlAndCommit(t *testing.T) {
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
	mux := http.NewServeMux()
	svc := &spanner.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: "root@" + project + ".iam.gserviceaccount.com", IsRoot: true}, true
	})

	instBase := "/v1/projects/" + project + "/instances"
	body := `{"instanceId":"sql-lab","instance":{"config":"projects/` + project + `/instanceConfigs/regional-us-central1","displayName":"SQL","nodeCount":1}}`
	req := httptest.NewRequest(http.MethodPost, instBase, bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create inst: %d %s", rec.Code, rec.Body.String())
	}
	dbBase := instBase + "/sql-lab/databases"
	req = httptest.NewRequest(http.MethodPost, dbBase, bytes.NewReader([]byte(`{"createStatement":"CREATE DATABASE `+"`"+`app`+"`"+`"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create db: %d %s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPatch, dbBase+"/app/ddl", bytes.NewReader([]byte(`{"statements":["CREATE TABLE T (id STRING(36)) PRIMARY KEY(id)"]}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ddl: %d %s", rec.Code, rec.Body.String())
	}

	sessURL := dbBase + "/app/sessions"
	req = httptest.NewRequest(http.MethodPost, sessURL, bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create session: %d %s", rec.Code, rec.Body.String())
	}
	var sess map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &sess)
	sessName, _ := sess["name"].(string)
	if sessName == "" {
		t.Fatalf("session=%#v", sess)
	}
	parts := strings.Split(sessName, "/")
	sessID := parts[len(parts)-1]

	req = httptest.NewRequest(http.MethodPost, dbBase+"/app/sessions:batchCreate",
		bytes.NewReader([]byte(`{"sessionCount":2}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("batchCreate: %d %s", rec.Code, rec.Body.String())
	}

	execURL := dbBase + "/app/sessions/" + sessID + ":executeSql"
	req = httptest.NewRequest(http.MethodPost, execURL, bytes.NewReader([]byte(`{"sql":"SELECT id FROM T"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("executeSql: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, dbBase+"/app/sessions/"+sessID+":commit",
		bytes.NewReader([]byte(`{"mutations":[{"insert":{"table":"T","columns":["id"],"values":[["row1"]]}}]}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("commit: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, execURL, bytes.NewReader([]byte(`{"sql":"SELECT id FROM T WHERE id = 'row1'"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("executeSql where: %d %s", rec.Code, rec.Body.String())
	}
}
