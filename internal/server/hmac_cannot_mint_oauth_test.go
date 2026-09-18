package server_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestHMACAuthCannotMintOAuthOrIAMCredentials(t *testing.T) {
	srv, _ := labForensicsServer(t, false, false)
	const host = "127.0.0.1:4588"
	email := "sa@noctaxris-gcp-local.iam.gserviceaccount.com"
	body := []byte(`{"scope":["https://www.googleapis.com/auth/cloud-platform"]}`)

	cases := []struct {
		name string
		path string
		host string
	}{
		{
			name: "iamcredentials path alias",
			path: "/iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/" + email + ":generateAccessToken",
			host: host,
		},
		{
			name: "token creator generateAccessToken",
			path: "/v1/projects/-/serviceAccounts/" + email + ":generateAccessToken",
			host: host,
		},
		{
			name: "iamcredentials host rewrite",
			path: "/v1/projects/-/serviceAccounts/" + email + ":generateAccessToken",
			host: "iamcredentials.googleapis.com",
		},
		{
			name: "iam signBlob",
			path: "/v1/projects/-/serviceAccounts/" + email + ":signBlob",
			host: host,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			auth, date := store.SignGOOG4HMACHeader(http.MethodPost, tc.host, tc.path,
				store.LabGCSHMACAccessID, store.LabGCSHMACSecret, time.Now().UTC())
			req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewReader(body))
			req.Host = tc.host
			req.Header.Set("Authorization", auth)
			req.Header.Set("x-goog-date", date)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}
