package pubsub

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/jwtutil"
	jose "github.com/go-jose/go-jose/v4"
)

func TestLabPushOIDCJWT(t *testing.T) {
	token := labPushOIDCJWT("sa@example.iam.gserviceaccount.com", "https://aud.example")
	if token == "" {
		t.Fatal("expected signed token")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[2] == "" {
		t.Fatalf("expected RS256 compact JWT with signature, got %q", token)
	}
	headerRaw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	var header map[string]any
	if err := json.Unmarshal(headerRaw, &header); err != nil {
		t.Fatal(err)
	}
	if header["alg"] != "RS256" {
		t.Fatalf("alg=%v want RS256", header["alg"])
	}
	if header["alg"] == "none" {
		t.Fatal("alg=none must be rejected")
	}
	jwks, err := jwtutil.MarshalLabOIDCJWKS()
	if err != nil {
		t.Fatal(err)
	}
	claims, err := jwtutil.VerifyCompactRS256(token, jwks)
	if err != nil {
		t.Fatal(err)
	}
	if claims["aud"] != "https://aud.example" || claims["email"] != "sa@example.iam.gserviceaccount.com" || claims["sub"] != "sa@example.iam.gserviceaccount.com" {
		t.Fatalf("claims=%#v", claims)
	}
}

func TestLabPushOIDCRejectsAlgNoneVerify(t *testing.T) {
	// Unsigned theatre tokens must not verify under RS256 allowlist.
	noneHeader := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"x"}`))
	token := noneHeader + "." + payload + "."
	jwks, err := jwtutil.MarshalLabOIDCJWKS()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := jwtutil.VerifyCompactRS256(token, jwks); err == nil {
		t.Fatal("expected alg=none reject")
	}
	_, err = jose.ParseSigned(token, jwtutil.AllowedRS256)
	if err == nil {
		t.Fatal("expected jose ParseSigned to reject alg=none")
	}
}
