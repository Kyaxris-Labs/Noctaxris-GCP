package authn_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
)

type memTokens struct {
	email string
	ok    bool
}

func (m memTokens) LookupAccessToken(tokenHash string, now time.Time) (string, bool, error) {
	_ = tokenHash
	_ = now
	return m.email, m.ok, nil
}

func TestAuthenticateRootBearer(t *testing.T) {
	a := &authn.Authenticator{
		RootServiceAccount: "root@noctaxris-gcp-local.iam.gserviceaccount.com",
		RootAccessToken:    "root-secret",
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/projects", nil)
	req.Header.Set("Authorization", "Bearer root-secret")
	p, err := a.AuthenticateRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if !p.IsRoot || p.Email != "root@noctaxris-gcp-local.iam.gserviceaccount.com" {
		t.Fatalf("principal = %+v", p)
	}
}

func TestAuthenticateRejectsMissingAndInvalid(t *testing.T) {
	a := &authn.Authenticator{
		RootServiceAccount: "root@noctaxris-gcp-local.iam.gserviceaccount.com",
		RootAccessToken:    "root-secret",
		Tokens:             memTokens{},
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/projects", nil)
	if _, err := a.AuthenticateRequest(req); err != authn.ErrUnauthenticated {
		t.Fatalf("missing auth: err = %v", err)
	}
	req.Header.Set("Authorization", "Bearer wrong")
	if _, err := a.AuthenticateRequest(req); err != authn.ErrUnauthenticated {
		t.Fatalf("wrong token: err = %v", err)
	}
	req.Header.Set("Authorization", "Basic abc")
	if _, err := a.AuthenticateRequest(req); err != authn.ErrUnauthenticated {
		t.Fatalf("non-bearer: err = %v", err)
	}
}

func TestAuthenticateRegisteredToken(t *testing.T) {
	a := &authn.Authenticator{
		RootAccessToken: "root-secret",
		Tokens:          memTokens{email: "sa@noctaxris-gcp-local.iam.gserviceaccount.com", ok: true},
	}
	p, err := a.AuthenticateToken("participant-token")
	if err != nil {
		t.Fatal(err)
	}
	if p.IsRoot || p.Email != "sa@noctaxris-gcp-local.iam.gserviceaccount.com" {
		t.Fatalf("principal = %+v", p)
	}
}

func TestIsPublicPath(t *testing.T) {
	if !authn.IsPublicPath("/_noctaxris-gcp/health") {
		t.Fatal("health should be public")
	}
	if !authn.IsPublicPath("/_noctaxris-gcp/http-catcher") {
		t.Fatal("http-catcher dump should be public")
	}
	if !authn.IsPublicPath("/_noctaxris-gcp/http-catcher/push") {
		t.Fatal("http-catcher accept prefix should be public")
	}
	if !authn.IsPublicPath("/identitytoolkit.googleapis.com/v1/accounts:signUp") {
		t.Fatal("Identity Toolkit client paths should be public")
	}
	if !authn.IsPublicPath("/v1/token") {
		t.Fatal("STS /v1/token should be public")
	}
	if !authn.IsPublicPath("/lb/p/fr/obj") {
		t.Fatal("expected public lb dataplane")
	}
	if !authn.IsPublicPath("/cdn/dist-id/obj") {
		t.Fatal("expected public cdn edge")
	}
	if authn.IsPublicPath("/v1/projects") {
		t.Fatal("API path should require auth")
	}
	if authn.IsPublicPath("/identitytoolkit.googleapis.com/v1/projects/p/accounts") {
		t.Fatal("admin Identity Toolkit paths should require auth")
	}
	// Prefix checks must use a cleaned path so /cdn/../ or /lb/../ cannot skip Bearer.
	if authn.IsPublicPath("/cdn/../v1/projects") {
		t.Fatal("cdn path traversal must not skip auth")
	}
	if authn.IsPublicPath("/lb/../storage/v1/b") {
		t.Fatal("lb path traversal must not skip auth")
	}
	if !authn.IsPublicPath("/cdn/dist/../dist/obj") {
		t.Fatal("cleaned cdn edge path should stay public")
	}
	if !authn.IsPublicPath("/_noctaxris-gcp/oidc-lab/.well-known/openid-configuration") {
		t.Fatal("oidc-lab discovery should be public")
	}
	if !authn.IsPublicPath("/_noctaxris-gcp/oidc-lab/.well-known/jwks.json") {
		t.Fatal("oidc-lab JWKS should be public")
	}
	if authn.IsPublicPath("/_noctaxris-gcp/oidc-lab/token") {
		t.Fatal("oidc-lab must not expose mint paths as public")
	}
}
