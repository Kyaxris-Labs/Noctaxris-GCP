package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/httpegress"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/server"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/iam"
)

func TestOIDCLabWellKnownUnauthenticated(t *testing.T) {
	srv, _ := testServer(t)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	for _, path := range []string{
		server.OIDCLabPath + "/.well-known/openid-configuration",
		server.OIDCLabPath + "/.well-known/jwks.json",
	} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s status=%d", path, resp.StatusCode)
		}
	}

	resp, err := http.Get(ts.URL + server.OIDCLabPath + "/token")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusNotFound {
		t.Fatalf("mint path status=%d want 401 or 404", resp.StatusCode)
	}
}

func TestOIDCLabSTSVerifyE2E(t *testing.T) {
	t.Setenv(iam.EnvSTSVerify, "1")
	t.Setenv(httpegress.EnvHTTPEgress, "")
	t.Setenv(httpegress.EnvHTTPAllowlist, "")

	srv, cfg := testServer(t)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	issuer := server.OIDCLabIssuerURL(ts.URL)
	rootAuth := "Bearer " + cfg.RootAccessToken
	project := cfg.ProjectID
	providerName := "projects/" + project + "/locations/global/workloadIdentityPools/oidc-lab-pool/providers/oidc"

	createPool, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/projects/"+project+"/locations/global/workloadIdentityPools?workloadIdentityPoolId=oidc-lab-pool",
		strings.NewReader(`{"displayName":"OIDC Lab Pool"}`))
	createPool.Header.Set("Authorization", rootAuth)
	createPool.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(createPool)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create pool status=%d body=%s", resp.StatusCode, body)
	}

	provBody := `{"displayName":"OIDC","oidc":{"issuerUri":"` + issuer + `"}}`
	createProv, _ := http.NewRequest(http.MethodPost,
		ts.URL+"/v1/projects/"+project+"/locations/global/workloadIdentityPools/oidc-lab-pool/providers?workloadIdentityPoolProviderId=oidc",
		strings.NewReader(provBody))
	createProv.Header.Set("Authorization", rootAuth)
	createProv.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(createProv)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create provider status=%d body=%s", resp.StatusCode, body)
	}

	discResp, err := http.Get(ts.URL + server.OIDCLabPath + "/.well-known/openid-configuration")
	if err != nil {
		t.Fatal(err)
	}
	discBody, _ := io.ReadAll(discResp.Body)
	_ = discResp.Body.Close()
	if discResp.StatusCode != http.StatusOK {
		t.Fatalf("discovery status=%d body=%s", discResp.StatusCode, discBody)
	}
	var disc map[string]any
	if err := json.Unmarshal(discBody, &disc); err != nil {
		t.Fatal(err)
	}
	if disc["issuer"] != issuer {
		t.Fatalf("issuer = %#v want %q", disc["issuer"], issuer)
	}
	jwksURI, _ := disc["jwks_uri"].(string)
	if jwksURI == "" {
		t.Fatalf("discovery = %#v", disc)
	}

	jwksResp, err := http.Get(jwksURI)
	if err != nil {
		t.Fatal(err)
	}
	jwksBody, _ := io.ReadAll(jwksResp.Body)
	_ = jwksResp.Body.Close()
	if jwksResp.StatusCode != http.StatusOK {
		t.Fatalf("jwks status=%d body=%s", jwksResp.StatusCode, jwksBody)
	}
	var jwks map[string]any
	if err := json.Unmarshal(jwksBody, &jwks); err != nil {
		t.Fatal(err)
	}
	keys, _ := jwks["keys"].([]any)
	if len(keys) == 0 {
		t.Fatalf("jwks = %#v", jwks)
	}

	now := time.Now().UTC()
	claims := map[string]any{
		"iss": issuer,
		"sub": "oidc-lab-alice",
		"aud": "//iam.googleapis.com/" + providerName,
		"exp": float64(now.Add(time.Hour).Unix()),
		"iat": float64(now.Unix()),
	}
	jwt, err := server.SignOIDCLabJWT(claims)
	if err != nil {
		t.Fatal(err)
	}

	form := "grant_type=" + url.QueryEscape(iam.GrantTypeTokenExchange) +
		"&audience=" + url.QueryEscape("//iam.googleapis.com/"+providerName) +
		"&subject_token=" + url.QueryEscape(jwt)
	exch, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/token", strings.NewReader(form))
	exch.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err = http.DefaultClient.Do(exch)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("STS exchange status=%d body=%s", resp.StatusCode, body)
	}
	var tok map[string]any
	if err := json.Unmarshal(body, &tok); err != nil {
		t.Fatal(err)
	}
	if tok["access_token"] == nil || tok["access_token"] == "" {
		t.Fatalf("token resp = %#v", tok)
	}
}
