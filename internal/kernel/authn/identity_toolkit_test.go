package authn_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
)

func TestIdentityToolkitIDTokenRoundTrip(t *testing.T) {
	authn.SetIdentityToolkitHMACKeyForTest([]byte("test-identity-toolkit-hmac-key!!"))
	tok, err := authn.MintIdentityToolkitIDToken("noctaxris-gcp-local", "uid-1", "u@example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	uid, ok := authn.LabIdentityToolkitUID(tok)
	if !ok || uid != "uid-1" {
		t.Fatalf("uid=%q ok=%v", uid, ok)
	}
}

func TestIdentityToolkitRejectsUnsigned(t *testing.T) {
	authn.SetIdentityToolkitHMACKeyForTest([]byte("test-identity-toolkit-hmac-key!!"))
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(
		`{"iss":"https://securetoken.google.com/noctaxris-gcp-local","user_id":"root@example.iam.gserviceaccount.com","sub":"root@example.iam.gserviceaccount.com"}`,
	))
	unsigned := header + "." + payload + "."
	if _, ok := authn.LabIdentityToolkitUID(unsigned); ok {
		t.Fatal("unsigned token must be rejected")
	}
	a := &authn.Authenticator{RootAccessToken: "root-token", RootServiceAccount: "root@example.iam.gserviceaccount.com"}
	if _, err := a.AuthenticateToken(unsigned); err != authn.ErrUnauthenticated {
		t.Fatalf("AuthenticateToken unsigned: %v", err)
	}
}

func TestIdentityToolkitReservedClaimsNotOverwritten(t *testing.T) {
	authn.SetIdentityToolkitHMACKeyForTest([]byte("test-identity-toolkit-hmac-key!!"))
	root := "root@noctaxris-gcp-local.iam.gserviceaccount.com"
	tok, err := authn.MintIdentityToolkitIDToken("noctaxris-gcp-local", "uid-victim", "v@example.com", map[string]any{
		"user_id": root,
		"sub":     root,
		"tier":    "gold",
	})
	if err != nil {
		t.Fatal(err)
	}
	uid, ok := authn.LabIdentityToolkitUID(tok)
	if !ok || uid != "uid-victim" {
		t.Fatalf("uid=%q ok=%v want uid-victim", uid, ok)
	}
	claims, ok := authn.VerifyIdentityToolkitIDToken(tok)
	if !ok {
		t.Fatal("verify failed")
	}
	if claims["tier"] != "gold" {
		t.Fatalf("custom claim missing: %#v", claims)
	}
	a := &authn.Authenticator{RootAccessToken: "root-token", RootServiceAccount: root}
	p, err := a.AuthenticateToken(tok)
	if err != nil {
		t.Fatal(err)
	}
	if p.Email != "uid-victim" {
		t.Fatalf("principal email=%q", p.Email)
	}
}

func TestIdentityToolkitCustomTokenRoundTrip(t *testing.T) {
	authn.SetIdentityToolkitHMACKeyForTest([]byte("test-identity-toolkit-hmac-key!!"))
	tok, err := authn.MintIdentityToolkitCustomToken("noctaxris-gcp-local", "uid-custom", map[string]any{"role": "lab"})
	if err != nil {
		t.Fatal(err)
	}
	uid, ok := authn.VerifyIdentityToolkitCustomToken(tok)
	if !ok || uid != "uid-custom" {
		t.Fatalf("uid=%q ok=%v", uid, ok)
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"uid":"forged","aud":"https://identitytoolkit.googleapis.com/google.identity.identitytoolkit.v1.IdentityToolkit"}`))
	if _, ok := authn.VerifyIdentityToolkitCustomToken(header + "." + payload + "."); ok {
		t.Fatal("unsigned custom token must be rejected")
	}
}

func TestIdentityToolkitEmailShapedUIDDoesNotMatchSABinding(t *testing.T) {
	authn.SetIdentityToolkitHMACKeyForTest([]byte("test-identity-toolkit-hmac-key!!"))
	root := "root@noctaxris-gcp-local.iam.gserviceaccount.com"
	tok, err := authn.MintIdentityToolkitIDToken("noctaxris-gcp-local", root, root, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := &authn.Authenticator{RootAccessToken: "other-root", RootServiceAccount: root}
	p, err := a.AuthenticateToken(tok)
	if err != nil {
		t.Fatal(err)
	}
	if p.Email != "user:"+root {
		t.Fatalf("email=%q want user: prefix", p.Email)
	}
	if p.IsRoot {
		t.Fatal("must not be root")
	}
}

func TestIdentityToolkitRequiresNumericExp(t *testing.T) {
	key := []byte("test-identity-toolkit-hmac-key!!")
	authn.SetIdentityToolkitHMACKeyForTest(key)
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	claims := map[string]any{
		"iss":     "https://securetoken.google.com/noctaxris-gcp-local",
		"user_id": "uid-1",
		"sub":     "uid-1",
		"exp":     "not-a-number",
	}
	raw, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	signingInput := header + "." + payload
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(signingInput))
	tok := signingInput + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if _, ok := authn.VerifyIdentityToolkitIDToken(tok); ok {
		t.Fatal("non-numeric exp must be rejected")
	}
}
