package server_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/config"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/server"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func labForensicsServer(t *testing.T, lab, logs bool) (*server.Server, config.Config) {
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
		LabForensics:       lab,
		LogsInject:         logs,
	}
	if err := st.EnsureRoot(cfg.ProjectID, cfg.RootServiceAccount); err != nil {
		t.Fatal(err)
	}
	return server.New(cfg, st, nil), cfg
}

func TestLabClockDeniedWhenDisabled(t *testing.T) {
	srv, cfg := labForensicsServer(t, false, false)
	req := httptest.NewRequest(http.MethodPost, "/_noctaxris-gcp/lab/clock:freeze", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestLabClockFreezeSetUnfreezeAndBulkSeed(t *testing.T) {
	srv, cfg := labForensicsServer(t, true, true)
	fixed := "2020-01-02T03:04:05Z"
	req := httptest.NewRequest(http.MethodPost, "/_noctaxris-gcp/lab/clock:set", bytes.NewReader([]byte(`{"fixedTime":"`+fixed+`"}`)))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("set status=%d body=%s", rec.Code, rec.Body.String())
	}

	t.Setenv("NOCTAXRIS_GCP_AUDIT_INJECT", "1")
	inject := `{"projectId":"` + cfg.ProjectID + `","entries":[{"serviceName":"iam.googleapis.com","methodName":"google.iam.admin.v1.GetRole","principalEmail":"alice@example.com"}]}`
	req = httptest.NewRequest(http.MethodPost, "/_noctaxris-gcp/lab/auditLogs:inject", bytes.NewReader([]byte(inject)))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("inject status=%d body=%s", rec.Code, rec.Body.String())
	}

	logName := "projects/" + cfg.ProjectID + "/logs/cloudaudit.googleapis.com%2Factivity"
	list := `{"resourceNames":["projects/` + cfg.ProjectID + `"],"filter":"logName=\"` + logName + `\""}`
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
	if len(resp.Entries) == 0 {
		t.Fatal("expected injected entry")
	}
	ts, _ := resp.Entries[0]["timestamp"].(string)
	if !strings.HasPrefix(ts, "2020-01-02T03:04:05") {
		t.Fatalf("timestamp=%q want lab clock", ts)
	}

	req = httptest.NewRequest(http.MethodPost, "/_noctaxris-gcp/lab/bulkSeed", bytes.NewReader([]byte(`{"scenarioId":"suspicious-login"}`)))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bulkSeed status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/_noctaxris-gcp/lab/clock:unfreeze", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unfreeze status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestComputeMetadataRequiresFlavor(t *testing.T) {
	srv, _ := labForensicsServer(t, false, false)
	req := httptest.NewRequest(http.MethodGet, "/computeMetadata/v1/instance/service-accounts/default/email", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing flavor status=%d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodGet, "/computeMetadata/v1/instance/service-accounts/default/email", nil)
	req.Header.Set("Metadata-Flavor", "Google")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("email status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "runtime@noctaxris-gcp-local.iam.gserviceaccount.com") {
		t.Fatalf("email=%s", rec.Body.String())
	}
}

func TestHMACAuthCannotCallIAMCredentials(t *testing.T) {
	srv, _ := labForensicsServer(t, false, false)
	auth, date := store.SignGOOG4HMACHeader("POST", "127.0.0.1:4588",
		"/iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/sa@x:generateAccessToken",
		store.LabGCSHMACAccessID, store.LabGCSHMACSecret, time.Now().UTC())
	req := httptest.NewRequest(http.MethodPost,
		"/iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/sa@x:generateAccessToken",
		bytes.NewReader([]byte(`{"scope":["https://www.googleapis.com/auth/cloud-platform"]}`)))
	req.Header.Set("Authorization", auth)
	req.Header.Set("x-goog-date", date)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("HMAC on IAM credentials status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestIamCredentialsHostAlias(t *testing.T) {
	srv, cfg := labForensicsServer(t, false, false)
	email := cfg.RootServiceAccount
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/-/serviceAccounts/"+email+":generateAccessToken",
		bytes.NewReader([]byte(`{"scope":["https://www.googleapis.com/auth/cloud-platform"]}`)))
	req.Host = "iamcredentials.googleapis.com"
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("host alias status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["accessToken"] == nil {
		t.Fatalf("missing accessToken %#v", out)
	}
}
