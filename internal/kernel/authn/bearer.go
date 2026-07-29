package authn

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	urlpath "path"
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
// The path is cleaned first so prefix checks cannot be bypassed with /cdn/../… or /lb/../….
func IsPublicPath(raw string) bool {
	if raw == "" {
		return false
	}
	path := urlpath.Clean(raw)
	if path == "." || !strings.HasPrefix(path, "/") {
		return false
	}
	switch path {
	case "/_noctaxris-gcp/health", "/_noctaxris-gcp/ready", "/_noctaxris-gcp/version",
		"/v1/token": // STS token exchange (subject_token authenticates)
		return true
	default:
		// Lab HTTP catcher accept + dump (Pub/Sub / Eventarc / Scheduler / Tasks theatre).
		if path == "/_noctaxris-gcp/http-catcher" || strings.HasPrefix(path, "/_noctaxris-gcp/http-catcher/") {
			return true
		}
		// Identity Toolkit client auth methods (Firebase Auth emulator shape).
		if strings.HasPrefix(path, "/identitytoolkit.googleapis.com/v1/accounts") {
			return true
		}
		// Lab load balancer and CDN edge dataplane (intentional public GET/HEAD).
		if strings.HasPrefix(path, "/lb/") || strings.HasPrefix(path, "/cdn/") {
			return true
		}
		return false
	}
}
