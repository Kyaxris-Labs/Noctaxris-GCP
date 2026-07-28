package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/config"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/audit"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/server"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "noctaxris-gcp: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "healthcheck" {
		return runHealthcheck()
	}

	cfg, err := config.LoadFromEnv()
	if err != nil {
		return err
	}
	if cfg.RootServiceAccount == "" || cfg.RootAccessToken == "" {
		return fmt.Errorf("set NOCTAXRIS_GCP_ROOT_SERVICE_ACCOUNT and NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN")
	}
	if config.ExampleRootCredentials(cfg.RootServiceAccount, cfg.RootAccessToken) && !config.ListenIsLoopback(cfg.ListenAddr) {
		return fmt.Errorf("example root credentials refused on non-loopback listen %q; set unique NOCTAXRIS_GCP_ROOT_SERVICE_ACCOUNT and NOCTAXRIS_GCP_ROOT_ACCESS_TOKEN (Compose binds 0.0.0.0 inside the container while host publish stays 127.0.0.1)", cfg.ListenAddr)
	}

	if err := os.MkdirAll(cfg.DataRoot, 0o700); err != nil {
		return fmt.Errorf("create data root: %w", err)
	}

	masterKeyPath, err := store.ResolveMasterKeyPath(cfg.MasterKeyPath, cfg.DataRoot)
	if err != nil {
		return fmt.Errorf("master key path: %w", err)
	}
	master, err := store.LoadOrCreateMasterKey(masterKeyPath)
	if err != nil {
		return fmt.Errorf("load master key: %w", err)
	}

	st, err := store.Open(cfg.DataRoot, master)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	if err := st.EnsureRoot(cfg.ProjectID, cfg.RootServiceAccount); err != nil {
		return fmt.Errorf("ensure root: %w", err)
	}

	aud, err := audit.NewWriter(filepath.Join(cfg.DataRoot, "audit"))
	if err != nil {
		return fmt.Errorf("open audit writer: %w", err)
	}
	defer aud.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := server.New(cfg, st, aud)
	return srv.ListenAndServeContext(ctx)
}

func runHealthcheck() error {
	addr := strings.TrimSpace(os.Getenv("NOCTAXRIS_GCP_LISTEN"))
	if addr == "" {
		addr = config.DefaultListenAddr
	}
	if strings.HasPrefix(addr, "0.0.0.0:") {
		addr = "127.0.0.1:" + strings.TrimPrefix(addr, "0.0.0.0:")
	}
	url := "http://" + addr + "/_noctaxris-gcp/ready"
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("healthcheck: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck: status %d", resp.StatusCode)
	}
	return nil
}
