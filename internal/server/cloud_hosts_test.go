package server_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/config"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/audit"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/server"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestListenAndServeContextCloudHostsStartsAndStops(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "secrets", "master.key")
	key, err := store.LoadOrCreateMasterKey(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "data"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	aud, err := audit.NewWriter(filepath.Join(dir, "audit"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = aud.Close() })
	cfg := config.Config{
		ListenAddr:         "127.0.0.1:0",
		DataRoot:           filepath.Join(dir, "data"),
		MasterKeyPath:      keyPath,
		RootServiceAccount: "root@noctaxris-gcp-local.iam.gserviceaccount.com",
		RootAccessToken:    "test-root-token",
		ProjectID:          "noctaxris-gcp-local",
		CloudHosts:         true,
		CloudHostsListen:   "127.0.0.1:0",
	}
	if err := st.EnsureRoot(cfg.ProjectID, cfg.RootServiceAccount); err != nil {
		t.Fatal(err)
	}
	srv := server.New(cfg, st, aud)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServeContext(ctx) }()
	time.Sleep(80 * time.Millisecond)
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("listen exit")
	}
}
