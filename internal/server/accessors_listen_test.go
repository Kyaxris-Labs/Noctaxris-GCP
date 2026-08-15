package server_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/server"
)

func TestServerAccessorsAndListenCancel(t *testing.T) {
	srv, _ := testServer(t)
	if srv.Authz() == nil {
		t.Fatal("Authz nil")
	}
	if srv.GRPC() == nil {
		t.Fatal("GRPC nil")
	}
	if id := server.RequestIDFromContext(context.Background()); id != "" {
		t.Fatalf("empty ctx request id=%q", id)
	}

	req := httptest.NewRequest(http.MethodPost, "/_noctaxris-gcp/health", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("health POST: %d", rec.Code)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServeContext(ctx)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-errCh:
		// Cancelled shutdown returns nil when the server closed cleanly.
		_ = err
	case <-time.After(3 * time.Second):
		t.Fatal("ListenAndServeContext did not return")
	}
}
