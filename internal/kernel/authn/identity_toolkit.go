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

// reservedIdentityToolkitClaims cannot be set via customAttributes / extra.
var reservedIdentityToolkitClaims = map[string]struct{}{
	"user_id": {}, "sub": {}, "iss": {}, "aud": {}, "iat": {}, "exp": {},
	"email": {}, "firebase": {}, "auth_time": {},
}

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

func signHS256(claims map[string]any) (string, error) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
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

// MintIdentityToolkitIDToken builds an HS256 Identity Toolkit id token.
// Reserved claims (user_id, sub, exp, …) are set after extra and cannot be overwritten.
func MintIdentityToolkitIDToken(projectID, localID, email string, extra map[string]any) (string, error) {
	projectID = strings.TrimSpace(projectID)
	localID = strings.TrimSpace(localID)
	if projectID == "" || localID == "" {
		return "", fmt.Errorf("project and localId required")
	}
	claims := map[string]any{}
	for k, v := range extra {
		if _, reserved := reservedIdentityToolkitClaims[k]; reserved {
			continue
		}
		claims[k] = v
	}
	claims["user_id"] = localID
	claims["sub"] = localID
	claims["email"] = email
	claims["firebase"] = map[string]any{
		"sign_in_provider": "password",
	}
	claims["iat"] = time.Now().Unix()
	claims["exp"] = time.Now().Add(time.Hour).Unix()
	claims["aud"] = projectID
	claims["iss"] = "https://securetoken.google.com/" + projectID
	return signHS256(claims)
}

// MintIdentityToolkitCustomToken builds an HS256 custom token for signInWithCustomToken.
func MintIdentityToolkitCustomToken(projectID, uid string, customClaims map[string]any) (string, error) {
	projectID = strings.TrimSpace(projectID)
	uid = strings.TrimSpace(uid)
	if projectID == "" || uid == "" {
		return "", fmt.Errorf("project and uid required")
	}
	claims := map[string]any{
		"uid": uid,
		"sub": uid,
		"iss": "noctaxris-gcp-lab@" + projectID + ".iam.gserviceaccount.com",
		"aud": "https://identitytoolkit.googleapis.com/google.identity.identitytoolkit.v1.IdentityToolkit",
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	if len(customClaims) > 0 {
		claims["claims"] = customClaims
	}
	return signHS256(claims)
}

// VerifyIdentityToolkitCustomToken validates an HS256 lab custom token and returns uid.
func VerifyIdentityToolkitCustomToken(token string) (uid string, ok bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[2] == "" {
		return "", false
	}
	hdrRaw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", false
	}
	var hdr map[string]any
	if err := json.Unmarshal(hdrRaw, &hdr); err != nil {
		return "", false
	}
	alg, _ := hdr["alg"].(string)
	if !strings.EqualFold(alg, "HS256") {
		return "", false
	}
	signingInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, identityToolkitKey())
	_, _ = mac.Write([]byte(signingInput))
	want := mac.Sum(nil)
	got, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(got, want) {
		return "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", false
	}
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		return "", false
	}
	aud, _ := claims["aud"].(string)
	if aud != "https://identitytoolkit.googleapis.com/google.identity.identitytoolkit.v1.IdentityToolkit" {
		return "", false
	}
	if !customTokenExpOK(claims["exp"]) {
		return "", false
	}
	uid, _ = claims["uid"].(string)
	if uid == "" {
		uid, _ = claims["sub"].(string)
	}
	if uid == "" {
		return "", false
	}
	return uid, true
}

func customTokenExpOK(exp any) bool {
	switch v := exp.(type) {
	case float64:
		return time.Now().Unix() <= int64(v)
	case json.Number:
		n, err := v.Int64()
		return err == nil && time.Now().Unix() <= n
	default:
		return false
	}
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
	if !customTokenExpOK(claims["exp"]) {
		return nil, false
	}
	return claims, true
}
