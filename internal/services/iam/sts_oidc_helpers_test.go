package iam

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"testing"
	"time"
)

func TestClaimExpiredAndNotYetValid(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	if !claimExpired(nil, now) {
		t.Fatal("nil claims should be expired")
	}
	if !claimExpired(map[string]any{}, now) {
		t.Fatal("missing exp should be expired")
	}
	if claimExpired(map[string]any{"exp": float64(now.Unix() + 60)}, now) {
		t.Fatal("future exp should not be expired")
	}
	if !claimExpired(map[string]any{"exp": float64(now.Unix() - 1)}, now) {
		t.Fatal("past exp should be expired")
	}

	if claimNotYetValid(nil, now) {
		t.Fatal("nil claims nbf")
	}
	if claimNotYetValid(map[string]any{"exp": float64(now.Unix() + 10)}, now) {
		t.Fatal("absent nbf should be valid")
	}
	if !claimNotYetValid(map[string]any{"nbf": "bad"}, now) {
		t.Fatal("invalid nbf should be not-yet-valid")
	}
	if claimNotYetValid(map[string]any{"nbf": float64(now.Unix() - 5)}, now) {
		t.Fatal("past nbf should be valid")
	}
	if !claimNotYetValid(map[string]any{"nbf": float64(now.Unix() + 30)}, now) {
		t.Fatal("future nbf should be not-yet-valid")
	}

	if _, ok := claimUnixSeconds(map[string]any{"exp": json.Number("123")}, "exp"); !ok || true {
		n, ok := claimUnixSeconds(map[string]any{"exp": json.Number("123")}, "exp")
		if !ok || n != 123 {
			t.Fatalf("json.Number got %d ok=%v", n, ok)
		}
	}
	n, ok := claimUnixSeconds(map[string]any{"exp": int64(9)}, "exp")
	if !ok || n != 9 {
		t.Fatalf("int64 got %d ok=%v", n, ok)
	}
	n, ok = claimUnixSeconds(map[string]any{"exp": 8}, "exp")
	if !ok || n != 8 {
		t.Fatalf("int got %d ok=%v", n, ok)
	}
	if _, ok := claimUnixSeconds(map[string]any{"exp": true}, "exp"); ok {
		t.Fatal("bool should fail")
	}
}

func TestVerifyCompactRS256AndRSAPublicFromJWK(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	n := base64.RawURLEncoding.EncodeToString(priv.N.Bytes())
	eBytes := big.NewInt(int64(priv.E)).Bytes()
	e := base64.RawURLEncoding.EncodeToString(eBytes)
	jwks, err := json.Marshal(map[string]any{
		"keys": []map[string]string{{
			"kty": "RSA", "kid": "k1", "alg": "RS256", "use": "sig", "n": n, "e": e,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"k1","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"user","exp":9999999999}`))
	signingInput := header + "." + payload
	sum := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	token := signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)

	claims, err := verifyCompactRS256(token, jwks)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if claims["sub"] != "user" {
		t.Fatalf("claims=%#v", claims)
	}

	if _, err := verifyCompactRS256("not.a.jwt.extra", jwks); err == nil {
		t.Fatal("expected malformed token error")
	}
	if _, err := verifyCompactRS256(token, []byte(`{"keys":[]}`)); err == nil {
		t.Fatal("expected empty jwks error")
	}
	badAlgHeader := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","kid":"k1"}`))
	if _, err := verifyCompactRS256(badAlgHeader+"."+payload+".x", jwks); err == nil {
		t.Fatal("expected alg error")
	}

	pub, err := rsaPublicFromJWK(n, e)
	if err != nil {
		t.Fatal(err)
	}
	if pub.E != priv.E {
		t.Fatalf("e=%d", pub.E)
	}
	if _, err := rsaPublicFromJWK("", e); err == nil {
		t.Fatal("expected empty modulus error")
	}
	if _, err := rsaPublicFromJWK(n, base64.RawURLEncoding.EncodeToString([]byte{0})); err == nil {
		t.Fatal("expected invalid exponent error")
	}
}
