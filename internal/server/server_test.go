package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/config"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/server"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func testServer(t *testing.T) (*server.Server, config.Config) {
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
	return server.New(cfg, st, nil), cfg
}

func TestHealthUnauthenticated(t *testing.T) {
	srv, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/_noctaxris-gcp/health", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "ok" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestReadyAndVersionUnauthenticated(t *testing.T) {
	srv, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/_noctaxris-gcp/ready", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ready status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "ready" {
		t.Fatalf("ready body = %q want ready", rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/_noctaxris-gcp/version", nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("version status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAPIWithoutBearerUnauthorized(t *testing.T) {
	srv, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v3/projects/noctaxris-gcp-local", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	errObj, _ := body["error"].(map[string]any)
	if errObj["status"] != "UNAUTHENTICATED" {
		t.Fatalf("error = %#v", errObj)
	}
}

func TestAPIWithRootBearerOK(t *testing.T) {
	srv, cfg := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v3/projects/"+cfg.ProjectID, nil)
	req.Header.Set("Authorization", "Bearer "+cfg.RootAccessToken)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHTTPCatcherDumpAfterPost(t *testing.T) {
	store.ClearHTTPCatcher()
	t.Cleanup(store.ClearHTTPCatcher)

	srv, _ := testServer(t)
	post := httptest.NewRequest(http.MethodPost, "/_noctaxris-gcp/http-catcher/lab", strings.NewReader(`{"ping":"catcher"}`))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, post)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST status=%d body=%s", rec.Code, rec.Body.String())
	}

	get := httptest.NewRequest(http.MethodGet, "/_noctaxris-gcp/http-catcher", nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, get)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", rec.Code, rec.Body.String())
	}
	var dump map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &dump); err != nil {
		t.Fatalf("decode dump: %v body=%s", err, rec.Body.String())
	}
	deliveries, _ := dump["deliveries"].([]any)
	if len(deliveries) != 1 || deliveries[0] != `{"ping":"catcher"}` {
		t.Fatalf("deliveries = %#v", dump["deliveries"])
	}
}
