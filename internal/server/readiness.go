package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/compute"
)

// handleReady matches Noctaxris: SQLite ping, optional nested-engine ping when
// NOCTAXRIS_GCP_DOCKER_HOST is set, body "ready" on success.
func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	if err := s.store.Ping(); err != nil {
		http.Error(w, "store not ready", http.StatusServiceUnavailable)
		return
	}
	if host := strings.TrimSpace(s.cfg.DockerHost); host != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		cli, err := compute.Dial(host, s.cfg.DockerTLSCertPath)
		if err != nil {
			http.Error(w, "engine client not ready", http.StatusServiceUnavailable)
			return
		}
		defer func() { _ = cli.Close() }()
		if err := cli.Ping(ctx); err != nil {
			http.Error(w, "engine not ready", http.StatusServiceUnavailable)
			return
		}
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready"))
}
