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

func setupLoggingCAL(t *testing.T, root bool) (*http.ServeMux, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "secrets", "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "data"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rootSA := "root@noctaxris-gcp-local.iam.gserviceaccount.com"
	if err := st.EnsureRoot("noctaxris-gcp-local", rootSA); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	svc := &logging.Service{Store: st, Authz: &authz.Evaluator{Policies: st}}
	svc.Mount(mux, func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: rootSA, IsRoot: root}, true
	})
	svc.MountLab(mux, func(r *http.Request) (authn.Principal, bool) {
		return authn.Principal{Email: rootSA, IsRoot: root}, true
	}, "noctaxris-gcp-local")
	return mux, st
}

func TestAuditInjectRequiresEnvAndRoot(t *testing.T) {
	t.Setenv(logging.EnvAuditInject, "")
	mux, _ := setupLoggingCAL(t, true)
	body := `{"entries":[{"serviceName":"storage.googleapis.com","methodName":"storage.buckets.get","principalEmail":"a@x"}]}`
	req := httptest.NewRequest(http.MethodPost, "/_noctaxris-gcp/lab/auditLogs:inject", bytes.NewReader([]byte(body)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("disabled inject status=%d body=%s", rec.Code, rec.Body.String())
	}

	t.Setenv(logging.EnvAuditInject, "1")
	mux, _ = setupLoggingCAL(t, false)
	req = httptest.NewRequest(http.MethodPost, "/_noctaxris-gcp/lab/auditLogs:inject", bytes.NewReader([]byte(body)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-root inject status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAuditInjectAndListViaLogging(t *testing.T) {
	t.Setenv(logging.EnvAuditInject, "1")
	mux, _ := setupLoggingCAL(t, true)
	project := "noctaxris-gcp-local"
	logName := "projects/" + project + "/logs/cloudaudit.googleapis.com%2Factivity"
	inject := `{
  "projectId":"` + project + `",
  "entries":[{
    "logName":"` + logName + `",
    "protoPayload":{
      "@type":"type.googleapis.com/google.cloud.audit.AuditLog",
      "serviceName":"storage.googleapis.com",
      "methodName":"storage.objects.get",
      "resourceName":"projects/_/buckets/lab/objects/o",
      "authenticationInfo":{"principalEmail":"alice@example.com"}
    }
  }]
}`
	req := httptest.NewRequest(http.MethodPost, "/_noctaxris-gcp/lab/auditLogs:inject", bytes.NewReader([]byte(inject)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("inject status=%d body=%s", rec.Code, rec.Body.String())
	}

	listBody := `{
  "resourceNames":["projects/` + project + `"],
  "filter":"logName=\"` + logName + `\""
}`
	req = httptest.NewRequest(http.MethodPost, "/v2/entries:list", bytes.NewReader([]byte(listBody)))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Entries []map[string]any `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Entries) != 1 {
		t.Fatalf("entries=%#v", resp.Entries)
	}
	pp, ok := resp.Entries[0]["protoPayload"].(map[string]any)
	if !ok || pp["methodName"] != "storage.objects.get" {
		t.Fatalf("protoPayload=%#v", resp.Entries[0]["protoPayload"])
	}

	req = httptest.NewRequest(http.MethodGet, "/v2/projects/"+project+"/logs", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list logs status=%d body=%s", rec.Code, rec.Body.String())
	}
	var logsResp struct {
		LogNames []string `json:"logNames"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &logsResp); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, n := range logsResp.LogNames {
		if n == logName {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("logNames=%v", logsResp.LogNames)
	}
}
