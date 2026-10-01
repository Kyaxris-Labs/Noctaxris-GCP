package authn

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	identityToolkitKeyOnce sync.Once
	identityToolkitHMACKey []byte
)

func identityToolkitKey() []byte {
	identityToolkitKeyOnce.Do(func() {
		identityToolkitHMACKey = make([]byte, 32)
		if _, err := rand.Read(identityToolkitHMACKey); err != nil {
			// Process must have entropy; fall back to a non-empty deterministic pad
			// only if rand fails (should not happen on supported platforms).
			sum := sha256.Sum256([]byte("noctaxris-gcp-identity-toolkit"))
			identityToolkitHMACKey = sum[:]
		}
	})
	return identityToolkitHMACKey
}

// SetIdentityToolkitHMACKeyForTest replaces the process HMAC key (tests only).
func SetIdentityToolkitHMACKeyForTest(key []byte) {
	identityToolkitKeyOnce.Do(func() {})
	identityToolkitHMACKey = append([]byte(nil), key...)
}

// MintIdentityToolkitIDToken builds an HS256 Identity Toolkit id token.
func MintIdentityToolkitIDToken(projectID, localID, email string, extra map[string]any) (string, error) {
	projectID = strings.TrimSpace(projectID)
	localID = strings.TrimSpace(localID)
	if projectID == "" || localID == "" {
		return "", fmt.Errorf("project and localId required")
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	claims := map[string]any{
		"user_id": localID,
		"sub":     localID,
		"email":   email,
		"firebase": map[string]any{
			"sign_in_provider": "password",
		},
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(time.Hour).Unix(),
		"aud": projectID,
		"iss": "https://securetoken.google.com/" + projectID,
	}
	for k, v := range extra {
		claims[k] = v
	}
	raw, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	signingInput := header + "." + payload
	mac := hmac.New(sha256.New, identityToolkitKey())
	_, _ = mac.Write([]byte(signingInput))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return signingInput + "." + sig, nil
}

// LabIdentityToolkitUID reports the localId from a verified Identity Toolkit id token.
// Unsigned tokens (alg none / empty signature) and bad signatures are rejected.
func LabIdentityToolkitUID(token string) (string, bool) {
	claims, ok := VerifyIdentityToolkitIDToken(token)
	if !ok {
		return "", false
	}
	uid, _ := claims["user_id"].(string)
	if uid == "" {
		uid, _ = claims["sub"].(string)
	}
	if uid == "" {
		return "", false
	}
	return uid, true
}

// VerifyIdentityToolkitIDToken validates HS256 signature, issuer prefix, and exp.
func VerifyIdentityToolkitIDToken(token string) (map[string]any, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, false
	}
	if parts[2] == "" {
		return nil, false
	}
	hdrRaw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, false
	}
	var hdr map[string]any
	if err := json.Unmarshal(hdrRaw, &hdr); err != nil {
		return nil, false
	}
	alg, _ := hdr["alg"].(string)
	if !strings.EqualFold(alg, "HS256") {
		return nil, false
	}
	signingInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, identityToolkitKey())
	_, _ = mac.Write([]byte(signingInput))
	want := mac.Sum(nil)
	got, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(got, want) {
		return nil, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, false
	}
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		return nil, false
	}
	iss, _ := claims["iss"].(string)
	if !strings.HasPrefix(iss, "https://securetoken.google.com/") {
		return nil, false
	}
	switch exp := claims["exp"].(type) {
	case float64:
		if time.Now().Unix() > int64(exp) {
			return nil, false
		}
	case json.Number:
		n, err := exp.Int64()
		if err != nil || time.Now().Unix() > n {
			return nil, false
		}
	}
	return claims, true
}
