package pubsub

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestLabPushOIDCJWT(t *testing.T) {
	token := labPushOIDCJWT("sa@example.iam.gserviceaccount.com", "https://aud.example")
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[2] != "" {
		t.Fatalf("expected unsigned JWT (empty sig), got %q", token)
	}
	headerRaw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	var header map[string]any
	if err := json.Unmarshal(headerRaw, &header); err != nil {
		t.Fatal(err)
	}
	if header["alg"] != "none" {
		t.Fatalf("alg=%v", header["alg"])
	}
	claimsRaw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims map[string]any
	if err := json.Unmarshal(claimsRaw, &claims); err != nil {
		t.Fatal(err)
	}
	if claims["aud"] != "https://aud.example" || claims["email"] != "sa@example.iam.gserviceaccount.com" || claims["sub"] != "sa@example.iam.gserviceaccount.com" {
		t.Fatalf("claims=%#v", claims)
	}
}
