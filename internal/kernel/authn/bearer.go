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

// ToolkitUserStatus reports whether an Identity Toolkit localId is disabled.
// ok=false means the user row is missing.
type ToolkitUserStatus interface {
	ToolkitUserDisabled(localID string) (disabled bool, ok bool, err error)
}

// Authenticator validates Authorization: Bearer tokens.
type Authenticator struct {
	RootServiceAccount string
	RootAccessToken    string
	Tokens             TokenLookup
	ToolkitUsers       ToolkitUserStatus
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
		if p, ok := identityToolkitPrincipalWithStatus(token, a.ToolkitUsers); ok {
			return p, nil
		}
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
		if p, ok := identityToolkitPrincipalWithStatus(token, a.ToolkitUsers); ok {
			return p, nil
		}
		return Principal{}, ErrUnauthenticated
	}
	return Principal{Email: email, IsRoot: false}, nil
}

func identityToolkitPrincipal(token string) (Principal, bool) {
	return identityToolkitPrincipalWithStatus(token, nil)
}

func identityToolkitPrincipalWithStatus(token string, users ToolkitUserStatus) (Principal, bool) {
	uid, ok := LabIdentityToolkitUID(token)
	if !ok {
		return Principal{}, false
	}
	if users != nil {
		disabled, found, err := users.ToolkitUserDisabled(uid)
		if err != nil {
			return Principal{}, false
		}
		if found && disabled {
			return Principal{}, false
		}
	}
	// Namespace every Toolkit localId so raw values cannot mint wif: / SA principals.
	return Principal{Email: toolkitPrincipalEmail(uid), IsRoot: false}, true
}

// toolkitPrincipalEmail returns the IAM member form for an Identity Toolkit localId.
// Always uses the user: prefix (idempotent if already prefixed).
func toolkitPrincipalEmail(localID string) string {
	localID = strings.TrimSpace(localID)
	if localID == "" {
		return ""
	}
	if strings.HasPrefix(localID, "user:") {
		return localID
	}
	return "user:" + localID
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
		"/v1/token",     // STS token exchange (subject_token authenticates)
		"/token",        // OAuth2 SA key JWT grant (assertion authenticates)
		"/oauth2/token": // accounts.google.com OAuth2 token alias (assertion authenticates)
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
		// Cloud Run nested HTTP browser route (served only while a nested container is up).
		if strings.HasPrefix(path, "/run/") {
			return true
		}
		// oidc-lab discovery/JWKS (STS verify self-fetch must not require Bearer).
		if strings.HasPrefix(path, "/_noctaxris-gcp/oidc-lab/.well-known/") {
			return true
		}
		if strings.HasPrefix(path, "/computeMetadata/v1") {
			return true
		}
		return false
	}
}
