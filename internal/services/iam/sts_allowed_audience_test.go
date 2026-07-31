package iam_test

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/httpegress"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/iam"
)

func TestSTSVerifyAcceptsStoredAllowedAudience(t *testing.T) {
	hh := openIAMHandler(t)
	t.Setenv(iam.EnvSTSVerify, "1")
	t.Setenv(httpegress.EnvHTTPEgress, "")
	t.Setenv(httpegress.EnvHTTPAllowlist, "")

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	const kid = "aud-key-1"
	jwksJSON, err := marshalTestJWKS(&key.PublicKey, kid)
	if err != nil {
		t.Fatal(err)
	}
	issuer := "http://127.0.0.1:4588/_noctaxris-gcp/sts-aud-test"
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
	pool, err := hh.store.CreateWIFPool(project, "global", "stored-aud-pool", "S", "", false)
	if err != nil {
		t.Fatal(err)
	}
	customAud := "https://my-service.example/audience"
	prov, err := hh.store.CreateWIFProvider(pool.Name, "oidc", "OIDC", "", issuer, "{}", `["`+customAud+`"]`, false)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	claims := map[string]any{
		"iss": issuer,
		"sub": "bob",
		"aud": customAud,
		"exp": float64(now.Add(time.Hour).Unix()),
	}
	jwt, err := signTestRS256(claims, key, kid)
	if err != nil {
		t.Fatal(err)
	}

	exchangeSTS(hh.mux, prov.Name, jwt, http.StatusOK, t)

	claims["aud"] = "https://wrong-audience"
	badJWT, err := signTestRS256(claims, key, kid)
	if err != nil {
		t.Fatal(err)
	}
	exchangeSTS(hh.mux, prov.Name, badJWT, http.StatusUnauthorized, t)
}

func TestSTSVerifyAcceptsStoredAudienceInAudArray(t *testing.T) {
	hh := openIAMHandler(t)
	t.Setenv(iam.EnvSTSVerify, "1")
	t.Setenv(httpegress.EnvHTTPEgress, "")
	t.Setenv(httpegress.EnvHTTPAllowlist, "")

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	const kid = "aud-arr-1"
	jwksJSON, err := marshalTestJWKS(&key.PublicKey, kid)
	if err != nil {
		t.Fatal(err)
	}
	issuer := "http://127.0.0.1:4588/_noctaxris-gcp/sts-aud-arr"
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
	pool, err := hh.store.CreateWIFPool(project, "global", "arr-aud-pool", "S", "", false)
	if err != nil {
		t.Fatal(err)
	}
	customAud := "https://svc.example/aud"
	prov, err := hh.store.CreateWIFProvider(pool.Name, "oidc", "OIDC", "", issuer, "{}", `["`+customAud+`"]`, false)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	claims := map[string]any{
		"iss": issuer,
		"sub": "carol",
		"aud": []any{"https://wrong.example", customAud},
		"exp": float64(now.Add(time.Hour).Unix()),
	}
	jwt, err := signTestRS256(claims, key, kid)
	if err != nil {
		t.Fatal(err)
	}
	exchangeSTS(hh.mux, prov.Name, jwt, http.StatusOK, t)
}

func TestSTSVerifyEmptyAllowedAudiencesOnlyProviderResourceAud(t *testing.T) {
	hh := openIAMHandler(t)
	t.Setenv(iam.EnvSTSVerify, "1")

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	const kid = "aud-key-2"
	jwksJSON, err := marshalTestJWKS(&key.PublicKey, kid)
	if err != nil {
		t.Fatal(err)
	}
	issuer := "http://127.0.0.1:4588/_noctaxris-gcp/sts-empty-aud-test"
	jwksURL := issuer + "/.well-known/jwks.json"
	hh.handler.STSFetch = func(raw string) ([]byte, error) {
		if raw == issuer+"/.well-known/openid-configuration" {
			return []byte(`{"jwks_uri":"` + jwksURL + `"}`), nil
		}
		if raw == jwksURL {
			return jwksJSON, nil
		}
		return nil, httpegress.ErrNotAllowed
	}

	const project = "noctaxris-gcp-local"
	pool, err := hh.store.CreateWIFPool(project, "global", "empty-aud-pool", "E", "", false)
	if err != nil {
		t.Fatal(err)
	}
	prov, err := hh.store.CreateWIFProvider(pool.Name, "oidc", "OIDC", "", issuer, "{}", "[]", false)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	okClaims := map[string]any{
		"iss": issuer,
		"sub": "carol",
		"aud": prov.Name,
		"exp": float64(now.Add(time.Hour).Unix()),
	}
	okJWT, err := signTestRS256(okClaims, key, kid)
	if err != nil {
		t.Fatal(err)
	}
	exchangeSTS(hh.mux, prov.Name, okJWT, http.StatusOK, t)

	badClaims := map[string]any{
		"iss": issuer,
		"sub": "carol",
		"aud": "https://not-listed",
		"exp": float64(now.Add(time.Hour).Unix()),
	}
	badJWT, err := signTestRS256(badClaims, key, kid)
	if err != nil {
		t.Fatal(err)
	}
	exchangeSTS(hh.mux, prov.Name, badJWT, http.StatusUnauthorized, t)
}

func TestSTSVerifyIgnoresCrossOriginJWKSURI(t *testing.T) {
	hh := openIAMHandler(t)
	t.Setenv(iam.EnvSTSVerify, "1")

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	const kid = "cross-origin-key"
	jwksJSON, err := marshalTestJWKS(&key.PublicKey, kid)
	if err != nil {
		t.Fatal(err)
	}
	issuer := "http://127.0.0.1:4588/_noctaxris-gcp/oidc-lab"
	goodJWKS := issuer + "/.well-known/jwks.json"
	evilJWKS := "https://evil.example/jwks.json"
	hh.handler.STSFetch = func(raw string) ([]byte, error) {
		switch raw {
		case issuer + "/.well-known/openid-configuration":
			return []byte(`{"jwks_uri":"` + evilJWKS + `"}`), nil
		case goodJWKS:
			return jwksJSON, nil
		case evilJWKS:
			t.Fatal("must not fetch cross-origin jwks_uri")
			return nil, httpegress.ErrNotAllowed
		default:
			return nil, httpegress.ErrNotAllowed
		}
	}

	const project = "noctaxris-gcp-local"
	pool, err := hh.store.CreateWIFPool(project, "global", "origin-pool", "O", "", false)
	if err != nil {
		t.Fatal(err)
	}
	prov, err := hh.store.CreateWIFProvider(pool.Name, "oidc", "OIDC", "", issuer, "", "[]", false)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	claims := map[string]any{
		"iss": issuer,
		"sub": "dave",
		"aud": prov.Name,
		"exp": float64(now.Add(time.Hour).Unix()),
	}
	jwt, err := signTestRS256(claims, key, kid)
	if err != nil {
		t.Fatal(err)
	}
	exchangeSTS(hh.mux, prov.Name, jwt, http.StatusOK, t)
}

func TestSTSVerifyIgnoresSameHostNonJWKSPath(t *testing.T) {
	hh := openIAMHandler(t)
	t.Setenv(iam.EnvSTSVerify, "1")

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	const kid = "same-host-path"
	jwksJSON, err := marshalTestJWKS(&key.PublicKey, kid)
	if err != nil {
		t.Fatal(err)
	}
	issuer := "http://127.0.0.1:4588/_noctaxris-gcp/oidc-lab"
	goodJWKS := issuer + "/.well-known/jwks.json"
	badSameHost := issuer + "/v1/projects/noctaxris-gcp-local"
	hh.handler.STSFetch = func(raw string) ([]byte, error) {
		switch raw {
		case issuer + "/.well-known/openid-configuration":
			return []byte(`{"jwks_uri":"` + badSameHost + `"}`), nil
		case goodJWKS:
			return jwksJSON, nil
		case badSameHost:
			t.Fatal("must not fetch same-host non-jwks path as jwks_uri")
			return nil, httpegress.ErrNotAllowed
		default:
			return nil, httpegress.ErrNotAllowed
		}
	}

	const project = "noctaxris-gcp-local"
	pool, err := hh.store.CreateWIFPool(project, "global", "path-pool", "P", "", false)
	if err != nil {
		t.Fatal(err)
	}
	prov, err := hh.store.CreateWIFProvider(pool.Name, "oidc", "OIDC", "", issuer, "", "[]", false)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	claims := map[string]any{
		"iss": issuer,
		"sub": "erin",
		"aud": prov.Name,
		"exp": float64(now.Add(time.Hour).Unix()),
	}
	jwt, err := signTestRS256(claims, key, kid)
	if err != nil {
		t.Fatal(err)
	}
	exchangeSTS(hh.mux, prov.Name, jwt, http.StatusOK, t)
}

func TestSTSVerifyIgnoresCrossOriginJWKSURIHTTPNoFetch(t *testing.T) {
	hh := openIAMHandler(t)
	t.Setenv(iam.EnvSTSVerify, "1")
	t.Setenv(httpegress.EnvHTTPEgress, "")
	t.Setenv(httpegress.EnvHTTPAllowlist, "")
	hh.handler.STSFetch = nil

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	const kid = "http-cross"
	jwksJSON, err := marshalTestJWKS(&key.PublicKey, kid)
	if err != nil {
		t.Fatal(err)
	}

	var evilHits int
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		evilHits++
		http.Error(w, "nope", http.StatusForbidden)
	}))
	t.Cleanup(evil.Close)

	mux := http.NewServeMux()
	mux.HandleFunc("/_noctaxris-gcp/oidc-lab/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		issuer := "http://" + r.Host + "/_noctaxris-gcp/oidc-lab"
		_ = json.NewEncoder(w).Encode(map[string]string{
			"issuer":   issuer,
			"jwks_uri": evil.URL + "/jwks.json",
		})
	})
	mux.HandleFunc("/_noctaxris-gcp/oidc-lab/.well-known/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jwksJSON)
	})
	issuerSrv := httptest.NewServer(mux)
	t.Cleanup(issuerSrv.Close)

	issuer := issuerSrv.URL + "/_noctaxris-gcp/oidc-lab"
	const project = "noctaxris-gcp-local"
	pool, err := hh.store.CreateWIFPool(project, "global", "http-origin-pool", "H", "", false)
	if err != nil {
		t.Fatal(err)
	}
	prov, err := hh.store.CreateWIFProvider(pool.Name, "oidc", "OIDC", "", issuer, "", "[]", false)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	claims := map[string]any{
		"iss": issuer,
		"sub": "frank",
		"aud": prov.Name,
		"exp": float64(now.Add(time.Hour).Unix()),
	}
	jwt, err := signTestRS256(claims, key, kid)
	if err != nil {
		t.Fatal(err)
	}
	exchangeSTS(hh.mux, prov.Name, jwt, http.StatusOK, t)
	if evilHits != 0 {
		t.Fatalf("cross-origin jwks was fetched %d times", evilHits)
	}
}

func exchangeSTS(mux http.Handler, providerName, jwt string, wantStatus int, t *testing.T) {
	body := "grant_type=" + url.QueryEscape(iam.GrantTypeTokenExchange) +
		"&audience=" + url.QueryEscape(providerName) +
		"&subject_token=" + url.QueryEscape(jwt)
	req := httptest.NewRequest(http.MethodPost, "/v1/token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != wantStatus {
		t.Fatalf("exchange want %d got %d body=%s", wantStatus, rec.Code, rec.Body.String())
	}
}
