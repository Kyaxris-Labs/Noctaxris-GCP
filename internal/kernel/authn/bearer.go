package authn

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"
)

// ErrUnauthenticated is returned when Bearer credentials are missing or invalid.
var ErrUnauthenticated = errors.New("unauthenticated")

// Principal is an authenticated caller.
type Principal struct {
	Email  string
	IsRoot bool
}

// TokenLookup resolves a registered access token hash to a principal email.
// Returns sql.ErrNoRows-shaped absence via ( "", false, nil ) or an error.
type TokenLookup interface {
	LookupAccessToken(tokenHash string, now time.Time) (principalEmail string, ok bool, err error)
}

// Authenticator validates Authorization: Bearer tokens.
type Authenticator struct {
	RootServiceAccount string
	RootAccessToken    string
	Tokens             TokenLookup
	Now                func() time.Time
}

// AuthenticateRequest extracts and validates the Bearer token from r.
func (a *Authenticator) AuthenticateRequest(r *http.Request) (Principal, error) {
	raw := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(raw, prefix) {
		return Principal{}, ErrUnauthenticated
	}
	token := strings.TrimSpace(strings.TrimPrefix(raw, prefix))
	if token == "" {
		return Principal{}, ErrUnauthenticated
	}
	return a.AuthenticateToken(token)
}

// AuthenticateToken validates a raw bearer token string.
func (a *Authenticator) AuthenticateToken(token string) (Principal, error) {
	if a.RootAccessToken != "" && token == a.RootAccessToken {
		email := a.RootServiceAccount
		if email == "" {
			email = "root"
		}
		return Principal{Email: email, IsRoot: true}, nil
	}
	if a.Tokens == nil {
		return Principal{}, ErrUnauthenticated
	}
	now := time.Now().UTC()
	if a.Now != nil {
		now = a.Now()
	}
	email, ok, err := a.Tokens.LookupAccessToken(HashToken(token), now)
	if err != nil {
		return Principal{}, err
	}
	if !ok || email == "" {
		return Principal{}, ErrUnauthenticated
	}
	return Principal{Email: email, IsRoot: false}, nil
}

// HashToken returns the hex-encoded SHA-256 digest of token.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// IsPublicPath reports whether path skips authentication.
func IsPublicPath(path string) bool {
	switch path {
	case "/_noctaxris-gcp/health", "/_noctaxris-gcp/ready", "/_noctaxris-gcp/version":
		return true
	default:
		return false
	}
}
