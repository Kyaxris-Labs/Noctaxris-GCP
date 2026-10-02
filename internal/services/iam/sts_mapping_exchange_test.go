package iam_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/httpegress"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/iam"
)

func compactUnsignedJWT(claims map[string]any) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	raw, _ := json.Marshal(claims)
	return header + "." + base64.RawURLEncoding.EncodeToString(raw) + "."
}

func TestSTSAttributeMappingExchange(t *testing.T) {
	h := openIAM(t)
	t.Setenv(iam.EnvSTSVerify, "")
	t.Setenv(httpegress.EnvHTTPEgress, "")
	t.Setenv(httpegress.EnvHTTPAllowlist, "")

	const project = "noctaxris-gcp-local"
	pool, err := h.store.CreateWIFPool(project, "global", "map-pool", "M", "", false)
	if err != nil {
		t.Fatal(err)
	}
	prov, err := h.store.CreateWIFProvider(
		pool.Name, "oidc", "OIDC", "", "",
		`{"google.subject":"assertion.sub"}`,
		`assertion.email == "ok@example.com"`,
		"[]", false,
	)
	if err != nil {
		t.Fatal(err)
	}

	token := compactUnsignedJWT(map[string]any{"sub": "mapped-user", "email": "ok@example.com"})
	body := "grant_type=" + url.QueryEscape(iam.GrantTypeTokenExchange) +
		"&audience=" + url.QueryEscape(prov.Name) +
		"&subject_token=" + url.QueryEscape(token)
	req := httptest.NewRequest(http.MethodPost, "/v1/token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	deny := compactUnsignedJWT(map[string]any{"sub": "mapped-user", "email": "nope@evil.com"})
	body = "grant_type=" + url.QueryEscape(iam.GrantTypeTokenExchange) +
		"&audience=" + url.QueryEscape(prov.Name) +
		"&subject_token=" + url.QueryEscape(deny)
	req = httptest.NewRequest(http.MethodPost, "/v1/token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("condition deny status=%d body=%s", rec.Code, rec.Body.String())
	}
}
