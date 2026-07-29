// Package labtoken mints registered lab Bearer tokens for interservice dispatch.
// Tokens use the same access_tokens hash table as IAM generateAccessToken (not Google-signed OIDC).
package labtoken

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
)

// TokenStore registers hashed access tokens (store.Store PutAccessToken).
type TokenStore interface {
	PutAccessToken(tokenHash, principalEmail string, expiresAt time.Time) error
}

// DefaultLifetime is the lab mint lifetime when callers pass zero.
const DefaultLifetime = time.Hour

// Mint generates a lab Bearer token (ngsa_…) for principalEmail and registers its SHA-256 hash.
func Mint(tokens TokenStore, principalEmail string, lifetime time.Duration) (token string, expire time.Time, err error) {
	email := strings.TrimSpace(principalEmail)
	if email == "" {
		return "", time.Time{}, fmt.Errorf("labtoken: service account email required")
	}
	if tokens == nil {
		return "", time.Time{}, fmt.Errorf("labtoken: token store required")
	}
	if lifetime <= 0 {
		lifetime = DefaultLifetime
	}
	token = newAccessToken()
	expire = time.Now().UTC().Add(lifetime)
	if err := tokens.PutAccessToken(authn.HashToken(token), email, expire); err != nil {
		return "", time.Time{}, fmt.Errorf("labtoken: register access token: %w", err)
	}
	return token, expire, nil
}

// DefaultComputeSAEmail returns the lab theatre default Compute Engine SA email for a project.
func DefaultComputeSAEmail(projectID string) string {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return ""
	}
	return projectID + "-compute@developer.gserviceaccount.com"
}

func newAccessToken() string {
	var b [32]byte
	_, _ = rand.Read(b[:])
	return "ngsa_" + hex.EncodeToString(b[:])
}
