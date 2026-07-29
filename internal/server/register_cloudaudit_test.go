package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/config"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/audit"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/logging"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func testServerWithCloudAudit(t *testing.T) (*Server, config.Config, *audit.Writer) {
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

	cfg := config.Config{
		ListenAddr:         "127.0.0.1:0",
		DataRoot:           filepath.Join(dir, "data"),
		RootServiceAccount: "root@noctaxris-gcp-local.iam.gserviceaccount.com",
		RootAccessToken:    "test-root-token",
		ProjectID:          "noctaxris-gcp-local",
	}
	if err := st.EnsureRoot(cfg.ProjectID, cfg.RootServiceAccount); err != nil {
		t.Fatal(err)
	}
	aud, err := audit.NewWriter(filepath.Join(dir, "audit"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = aud.Close() })

	srv := New(cfg, st, aud)
	return srv, cfg, aud
}

func TestRegisterCloudAuditInjectAndList(t *testing.T) {
	t.Setenv(logging.EnvAuditInject, "1")
	srv, cfg, _ := testServerWithCloudAudit(t)
	project := cfg.ProjectID
	logName := "projects/" + project + "/logs/cloudaudit.googleapis.com%2Factivity"
	body := `{"projectId":"` + project + `","entries":[{"serviceName":"iam.googleapis.com","methodName":"google.iam.admin.v1.GetServiceAccount","principalEmail":"alice@example.com","resourceName":"projects/` + project + `/serviceAccounts/sa@` + project + `.iam.gserviceaccount.com"}]}`
	req := httptest.NewRequest(http.MethodPost, "/_noctaxris-gcp/lab/auditLogs:inject", bytes.NewReader([]byte(body)))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("inject status=%d body=%s", rec.Code, rec.Body.String())
	}

	list := `{"resourceNames":["projects/` + project + `"],"filter":"logName=\"` + logName + `\""}`
	req = httptest.NewRequest(http.MethodPost, "/v2/entries:list", bytes.NewReader([]byte(list)))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
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
}

func TestRegisterCloudAuditLiveWriterSink(t *testing.T) {
	t.Setenv(logging.EnvAuditInject, "1")
	srv, cfg, aud := testServerWithCloudAudit(t)
	if err := aud.Write(context.Background(), audit.Event{
		InsertID: "sink-1", Timestamp: time.Now().UTC(),
		ServiceName: "logging.googleapis.com", MethodName: "google.logging.v2.WriteLogEntries",
		PrincipalEmail: cfg.RootServiceAccount, ResourceName: "projects/" + cfg.ProjectID,
	}); err != nil {
		t.Fatal(err)
	}

	logName := "projects/" + cfg.ProjectID + "/logs/cloudaudit.googleapis.com%2Factivity"
	list := `{"resourceNames":["projects/` + cfg.ProjectID + `"],"filter":"logName=\"` + logName + `\""}`
	req := httptest.NewRequest(http.MethodPost, "/v2/entries:list", bytes.NewReader([]byte(list)))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Entries []map[string]any `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Entries) < 1 {
		t.Fatalf("expected sink-mirrored CAL entry, got %#v", resp.Entries)
	}
}
