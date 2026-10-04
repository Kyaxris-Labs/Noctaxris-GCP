package server_test

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/config"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/server"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func testServerStrip(t *testing.T, strip bool) (*server.Server, config.Config) {
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
		StripProduct:       strip,
	}
	display := cfg.RootSADisplayName()
	if err := st.EnsureRoot(cfg.ProjectID, cfg.RootServiceAccount, display); err != nil {
		t.Fatal(err)
	}
	sa, ok, err := st.GetServiceAccount(cfg.RootServiceAccount)
	if err != nil || !ok {
		t.Fatalf("get root sa: ok=%v err=%v", ok, err)
	}
	if sa.DisplayName != display {
		t.Fatalf("root displayName = %q want %q", sa.DisplayName, display)
	}
	return server.New(cfg, st, nil), cfg
}

func TestStripProductHealthPaths(t *testing.T) {
	srv, _ := testServerStrip(t, true)

	req := httptest.NewRequest(http.MethodGet, "/_lab/health", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("health status=%d body=%q", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/_lab/ready", nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "ready" {
		t.Fatalf("ready status=%d body=%q", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/_lab/version", nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("version status=%d body=%s", rec.Code, rec.Body.String())
	}

	for _, path := range []string{"/_noctaxris-gcp/health", "/_noctaxris-gcp/ready", "/_noctaxris-gcp/version"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d want 404 body=%s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestDefaultHealthPathsStillRegistered(t *testing.T) {
	srv, _ := testServerStrip(t, false)
	req := httptest.NewRequest(http.MethodGet, "/_noctaxris-gcp/health", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/_lab/health", nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("/_lab/health status = %d want 404", rec.Code)
	}
}
