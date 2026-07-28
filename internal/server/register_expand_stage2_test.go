package server_test

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
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/workflows"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// stage2Mux mounts Workflows + Spanner the same way registerExpandStage2 does,
// without constructing the full Server (peer Stage 2 mounts may conflict on shared paths).
func stage2Mux(t *testing.T) (*http.ServeMux, *store.Store, string) {
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
	root := "root@noctaxris-gcp-local.iam.gserviceaccount.com"
	if err := st.EnsureRoot("noctaxris-gcp-local", root); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	principalFrom := func(*http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: root, IsRoot: true}, true
	}
	eval := &authz.Evaluator{Policies: st}
	(&workflows.Service{Store: st, Authz: eval}).Mount(mux, principalFrom)
	(&spanner.Service{Store: st, Authz: eval}).Mount(mux, principalFrom)
	return mux, st, "noctaxris-gcp-local"
}

func TestWorkflowsViaServer(t *testing.T) {
	mux, _, project := stage2Mux(t)
	loc := workflows.DefaultLocation
	base := "/v1/projects/" + project + "/locations/" + loc + "/workflows"
	body := `{"sourceContents":"main:\n  steps:\n    - done:\n        return: ok\n"}`
	req := httptest.NewRequest(http.MethodPost, base+"?workflowId=server-wf", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, base+"/server-wf/executions", bytes.NewReader([]byte(`{"argument":"{}"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("execution status=%d body=%s", rec.Code, rec.Body.String())
	}
	var ex map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &ex)
	if ex["state"] != "SUCCEEDED" {
		t.Fatalf("execution=%#v", ex)
	}
}

func TestSpannerViaServer(t *testing.T) {
	mux, _, project := stage2Mux(t)
	instBase := "/v1/projects/" + project + "/instances"
	body := `{"instanceId":"srv","instance":{"config":"projects/` + project + `/instanceConfigs/regional-us-central1","displayName":"Srv","nodeCount":1}}`
	req := httptest.NewRequest(http.MethodPost, instBase, bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create instance status=%d body=%s", rec.Code, rec.Body.String())
	}

	dbBody := `{"createStatement":"CREATE DATABASE ` + "`" + `db1` + "`" + `"}`
	req = httptest.NewRequest(http.MethodPost, instBase+"/srv/databases", bytes.NewReader([]byte(dbBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create database status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, instBase+"/srv/databases/db1/sessions", bytes.NewReader([]byte("{}")))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create session status=%d body=%s", rec.Code, rec.Body.String())
	}
	var sess map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &sess)
	name, _ := sess["name"].(string)
	parts := bytes.Split([]byte(name), []byte("/"))
	sessID := string(parts[len(parts)-1])

	req = httptest.NewRequest(http.MethodPost, instBase+"/srv/databases/db1/sessions/"+sessID+":executeSql", bytes.NewReader([]byte(`{"sql":"SELECT 1"}`)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("executeSql status=%d body=%s", rec.Code, rec.Body.String())
	}
}
