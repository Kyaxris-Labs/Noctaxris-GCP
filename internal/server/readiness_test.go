package server_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReadyBodyMatchesNoctaxris(t *testing.T) {
	srv, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/_noctaxris-gcp/ready", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != "ready" {
		t.Fatalf("body=%q want ready (Noctaxris-compatible)", got)
	}
}
