package server_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/config"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/server"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestGKEEdgeRegisteredOnServer(t *testing.T) {
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
	cfg := config.Config{
		ListenAddr:         ":0",
		ProjectID:          "noctaxris-gcp-local",
		RootServiceAccount: "root@noctaxris-gcp-local.iam.gserviceaccount.com",
		RootAccessToken:    "lab-root-token",
	}
	srv := server.New(cfg, st, nil)
	h := srv.Handler()

	req := httptest.NewRequest(http.MethodPost,
		"/container/v1/projects/noctaxris-gcp-local/locations/us-central1/clusters?clusterId=srv-gke",
		bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Authorization", "Bearer lab-root-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("gke create status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/cdn/missing-edge/obj", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cdn edge without auth expected 404, got %d", rec.Code)
	}
}
