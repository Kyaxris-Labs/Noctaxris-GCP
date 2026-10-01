package authn_test

import (
	"encoding/base64"
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
