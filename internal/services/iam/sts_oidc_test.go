package iam_test

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/httpegress"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/iam"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func TestSTSTheatreAcceptsAnySubjectToken(t *testing.T) {
	h := openIAM(t)
	t.Setenv(iam.EnvSTSVerify, "")
	t.Setenv(httpegress.EnvHTTPEgress, "")
	t.Setenv(httpegress.EnvHTTPAllowlist, "")

	const project = "noctaxris-gcp-local"
	pool, err := h.store.CreateWIFPool(project, "global", "theatre-pool", "T", "", false)
	if err != nil {
		t.Fatal(err)
	}
	prov, err := h.store.CreateWIFProvider(pool.Name, "oidc", "OIDC", "", "https://example.com", "", false)
	if err != nil {
		t.Fatal(err)
	}

	body := "grant_type=" + url.QueryEscape(iam.GrantTypeTokenExchange) +
		"&audience=" + url.QueryEscape(prov.Name) +
		"&subject_token=lab-theatre-sub"
	req := httptest.NewRequest(http.MethodPost, "/v1/token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("theatre exchange status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSTSVerifyRejectsBadSubjectToken(t *testing.T) {
	h := openIAM(t)
	t.Setenv(iam.EnvSTSVerify, "1")
	t.Setenv(httpegress.EnvHTTPEgress, "")
	t.Setenv(httpegress.EnvHTTPAllowlist, "")

	const project = "noctaxris-gcp-local"
	pool, err := h.store.CreateWIFPool(project, "global", "verify-pool", "V", "", false)
	if err != nil {
		t.Fatal(err)
	}
	prov, err := h.store.CreateWIFProvider(pool.Name, "oidc", "OIDC", "", "https://example.com", "", false)
	if err != nil {
		t.Fatal(err)
	}

	body := "grant_type=" + url.QueryEscape(iam.GrantTypeTokenExchange) +
		"&audience=" + url.QueryEscape(prov.Name) +
		"&subject_token=not-a-jwt"
	req := httptest.NewRequest(http.MethodPost, "/v1/token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("verify reject status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid subject_token") {
		t.Fatalf("expected invalid subject_token message, body=%s", rec.Body.String())
	}
}

func TestSTSVerifyAcceptsSignedJWTViaLabJWKS(t *testing.T) {
	hh := openIAMHandler(t)
	t.Setenv(iam.EnvSTSVerify, "1")
	t.Setenv(httpegress.EnvHTTPEgress, "")
	t.Setenv(httpegress.EnvHTTPAllowlist, "")

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	const kid = "lab-oidc-1"
	jwksJSON, err := marshalTestJWKS(&key.PublicKey, kid)
	if err != nil {
		t.Fatal(err)
	}

	issuer := "http://127.0.0.1:4588/_noctaxris-gcp/oidc-lab"
	discoveryURL := issuer + "/.well-known/openid-configuration"
	jwksURL := issuer + "/.well-known/jwks.json"
	hh.handler.STSFetch = func(raw string) ([]byte, error) {
		switch raw {
		case discoveryURL:
			return []byte(`{"jwks_uri":"` + jwksURL + `"}`), nil
		case jwksURL:
			return jwksJSON, nil
		default:
			return nil, httpegress.ErrNotAllowed
		}
	}

	const project = "noctaxris-gcp-local"
	pool, err := hh.store.CreateWIFPool(project, "global", "jwks-pool", "J", "", false)
	if err != nil {
		t.Fatal(err)
	}
	prov, err := hh.store.CreateWIFProvider(pool.Name, "oidc", "OIDC", "", issuer, "", false)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	claims := map[string]any{
		"iss": issuer,
		"sub": "alice",
		"aud": "//iam.googleapis.com/" + prov.Name,
		"exp": float64(now.Add(time.Hour).Unix()),
		"iat": float64(now.Unix()),
	}
	jwt, err := signTestRS256(claims, key, kid)
	if err != nil {
		t.Fatal(err)
	}

	body := "grant_type=" + url.QueryEscape(iam.GrantTypeTokenExchange) +
		"&audience=" + url.QueryEscape("//iam.googleapis.com/"+prov.Name) +
		"&subject_token=" + url.QueryEscape(jwt)
	req := httptest.NewRequest(http.MethodPost, "/v1/token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	hh.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("verify accept status=%d body=%s", rec.Code, rec.Body.String())
	}
}

type iamHandlerHarness struct {
	mux     *http.ServeMux
	store   *store.Store
	handler *iam.Handler
}

func openIAMHandler(t *testing.T) *iamHandlerHarness {
	t.Helper()
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
	const project = "noctaxris-gcp-local"
	root := "root@" + project + ".iam.gserviceaccount.com"
	if err := st.EnsureRoot(project, root); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	ih := &iam.Handler{
		Store: st,
		Authz: &authz.Evaluator{Policies: st},
		Principal: func(r *http.Request) (authn.Principal, bool) {
			return authn.Principal{}, false
		},
	}
	ih.Mount(mux)
	return &iamHandlerHarness{mux: mux, store: st, handler: ih}
}

func marshalTestJWKS(pub *rsa.PublicKey, kid string) ([]byte, error) {
	n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
	eb := big.NewInt(int64(pub.E)).Bytes()
	e := base64.RawURLEncoding.EncodeToString(eb)
	return json.Marshal(map[string]any{
		"keys": []map[string]string{{
			"kty": "RSA",
			"kid": kid,
			"alg": "RS256",
			"use": "sig",
			"n":   n,
			"e":   e,
		}},
	})
}

func signTestRS256(claims map[string]any, key *rsa.PrivateKey, kid string) (string, error) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT","kid":"` + kid + `"}`))
	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	signingInput := header + "." + payload
	sum := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}
