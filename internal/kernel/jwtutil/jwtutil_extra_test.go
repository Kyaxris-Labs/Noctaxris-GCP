package jwtutil_test

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/jwtutil"
)

func TestParseJWKSBoundaries(t *testing.T) {
	if _, err := jwtutil.ParseJWKS(nil); err == nil {
		t.Fatal("empty JWKS must fail")
	}
	if _, err := jwtutil.ParseJWKS([]byte(`{`)); err == nil {
		t.Fatal("malformed JWKS must fail")
	}
	if _, err := jwtutil.ParseJWKS([]byte(`{"keys":[]}`)); err == nil {
		t.Fatal("empty keys must fail")
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jwtutil.MarshalJWKS(&key.PublicKey, "kid-1")
	if err != nil {
		t.Fatal(err)
	}
	set, err := jwtutil.ParseJWKS(raw)
	if err != nil || len(set.Keys) != 1 || set.Keys[0].KeyID != "kid-1" {
		t.Fatalf("parse ok got err=%v set=%v", err, set)
	}
}

func TestSignRS256RejectsNilKeyAndEmptyKid(t *testing.T) {
	if _, err := jwtutil.SignRS256([]byte(`{}`), nil, "kid"); err == nil {
		t.Fatal("nil key must fail")
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := jwtutil.SignRS256([]byte(`{}`), key, ""); err == nil {
		t.Fatal("empty kid must fail")
	}
}

func TestSignHS256EmptyKeyAndVerifyEmpty(t *testing.T) {
	if _, err := jwtutil.SignHS256([]byte(`{}`), nil); err == nil {
		t.Fatal("empty hmac key must fail")
	}
	key := []byte("0123456789abcdef0123456789abcdef")
	token, err := jwtutil.SignHS256([]byte(`{"sub":"x"}`), key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := jwtutil.VerifyCompactHS256("", key); err == nil {
		t.Fatal("empty token must fail")
	}
	if _, err := jwtutil.VerifyCompactHS256(token, nil); err == nil {
		t.Fatal("empty verify key must fail")
	}
	if _, err := jwtutil.VerifyCompactRS256("", rawJWKS(t)); err == nil {
		t.Fatal("empty RS256 token must fail")
	}
}

func rawJWKS(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jwtutil.MarshalJWKS(&key.PublicKey, "k")
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestDecodeCompactClaimsUnverifiedAndClaimHelpers(t *testing.T) {
	if _, err := jwtutil.DecodeCompactClaimsUnverified("a.b"); err == nil {
		t.Fatal("not compact must fail")
	}
	if _, err := jwtutil.DecodeCompactClaimsUnverified("a.!!!.c"); err == nil {
		t.Fatal("bad payload b64 must fail")
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	payload, _ := json.Marshal(map[string]any{
		"sub": "peek",
		"n":   float64(7),
		"exp": now.Add(time.Hour).Unix(),
	})
	token, err := jwtutil.SignRS256(payload, key, "kid")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := jwtutil.DecodeCompactClaimsUnverified(token)
	if err != nil {
		t.Fatal(err)
	}
	if jwtutil.ClaimString(claims, "sub") != "peek" || jwtutil.ClaimString(claims, "n") != "7" {
		t.Fatalf("claims=%v", claims)
	}
	if jwtutil.ClaimString(nil, "sub") != "" || jwtutil.ClaimString(claims, "missing") != "" {
		t.Fatal("missing claim must be empty")
	}
	if jwtutil.ClaimString(map[string]any{"x": true}, "x") == "" {
		t.Fatal("bool claim should stringify")
	}
	if jwtutil.ClaimExpired(claims, now) {
		t.Fatal("fresh token must not be expired")
	}
	if !jwtutil.ClaimExpired(map[string]any{"exp": now.Add(-time.Hour).Unix()}, now) {
		t.Fatal("past exp must be expired")
	}
	if !jwtutil.ClaimExpired(map[string]any{}, now) || !jwtutil.ClaimExpired(map[string]any{"exp": "bad"}, now) {
		t.Fatal("missing/non-numeric exp must fail closed")
	}
	if jwtutil.ClaimNotYetValid(nil, now) {
		t.Fatal("nil claims nbf optional")
	}
	if !jwtutil.ClaimNotYetValid(map[string]any{"nbf": json.Number("x")}, now) {
		t.Fatal("bad json.Number nbf must fail closed")
	}
	if jwtutil.ClaimNotYetValid(map[string]any{"nbf": int64(now.Add(-time.Minute).Unix())}, now) {
		t.Fatal("int64 past nbf valid")
	}
	if jwtutil.ClaimNotYetValid(map[string]any{"nbf": int(now.Add(-time.Minute).Unix())}, now) {
		t.Fatal("int past nbf valid")
	}
}

func TestLabOIDCSignAndJWKS(t *testing.T) {
	token, err := jwtutil.SignLabOIDCRS256(map[string]any{
		"iss": "https://lab.example/oidc",
		"sub": "lab-user",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	jwks, err := jwtutil.MarshalLabOIDCJWKS()
	if err != nil {
		t.Fatal(err)
	}
	claims, err := jwtutil.VerifyCompactRS256(token, jwks)
	if err != nil {
		t.Fatal(err)
	}
	if jwtutil.ClaimString(claims, "sub") != "lab-user" {
		t.Fatalf("sub=%v", claims["sub"])
	}
	key, err := jwtutil.LabOIDCPrivateKey()
	if err != nil || key == nil {
		t.Fatalf("lab key err=%v", err)
	}
}

func TestVerifyCompactRejectsBadClaimsJSON(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	token, err := jwtutil.SignRS256([]byte(`not-json`), key, "kid")
	if err != nil {
		t.Fatal(err)
	}
	jwks, err := jwtutil.MarshalJWKS(&key.PublicKey, "kid")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := jwtutil.VerifyCompactRS256(token, jwks); err == nil {
		t.Fatal("non-object payload must fail claims parse")
	}
	hsKey := []byte("0123456789abcdef0123456789abcdef")
	hsTok, err := jwtutil.SignHS256([]byte(`not-json`), hsKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := jwtutil.VerifyCompactHS256(hsTok, hsKey); err == nil {
		t.Fatal("HS256 non-object payload must fail")
	}
}
