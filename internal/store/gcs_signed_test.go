package store_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestGCSV4SignedURLDenyPaths(t *testing.T) {
	host := "127.0.0.1:4588"
	path := "/storage/v1/b/lab/o/obj.txt"
	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	q := url.Values{"alt": []string{"media"}}

	signed, err := store.GenerateV4SignedURL(store.SignedURLRequest{
		Method:  "GET",
		Host:    host,
		Path:    path,
		Expires: 300,
		Query:   q,
		Now:     now,
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(signed)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.VerifyV4SignedRequest(http.MethodPost, host, path, u.Query(), now.Add(time.Minute)); err == nil {
		t.Fatal("expected method mismatch failure")
	}

	tampered := u.Query()
	sig := tampered.Get("X-Goog-Signature")
	tampered.Set("X-Goog-Signature", strings.Repeat("0", len(sig)))
	if err := store.VerifyV4SignedRequest(http.MethodGet, host, path, tampered, now.Add(time.Minute)); err == nil {
		t.Fatal("expected signature mismatch failure")
	}

	if err := store.VerifyV4SignedRequest(http.MethodGet, "evil.example", path, u.Query(), now.Add(time.Minute)); err == nil {
		t.Fatal("expected host mismatch failure")
	}

	if !store.HasV4Signature(u.Query()) {
		t.Fatal("expected HasV4Signature true")
	}
	empty := url.Values{}
	if store.HasV4Signature(empty) {
		t.Fatal("expected HasV4Signature false for empty query")
	}
}
