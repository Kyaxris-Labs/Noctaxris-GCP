package authn_test

import (
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
)

type memMapToolkitUsers map[string]bool

func (m memMapToolkitUsers) ToolkitUserDisabled(localID string) (disabled bool, ok bool, err error) {
	d, found := m[localID]
	return d, found, nil
}

func TestDisabledToolkitIDTokenRejected(t *testing.T) {
	authn.SetIdentityToolkitHMACKeyForTest([]byte("test-identity-toolkit-hmac-key!!"))
	tok, err := authn.MintIdentityToolkitIDToken("noctaxris-gcp-local", "uid-disabled", "d@example.com", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	a := &authn.Authenticator{
		ToolkitUsers: memMapToolkitUsers{"uid-disabled": true},
	}
	if _, err := a.AuthenticateToken(tok); err == nil {
		t.Fatal("disabled toolkit user idToken must fail")
	}
	a.ToolkitUsers = memMapToolkitUsers{"uid-disabled": false}
	p, err := a.AuthenticateToken(tok)
	if err != nil {
		t.Fatal(err)
	}
	if p.Email != "user:uid-disabled" {
		t.Fatalf("email=%q", p.Email)
	}
}
