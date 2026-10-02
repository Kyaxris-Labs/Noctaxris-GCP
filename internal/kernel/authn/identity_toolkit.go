package authn

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/jwtutil"
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
	raw, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	return jwtutil.SignHS256(raw, identityToolkitKey())
}

// MintIdentityToolkitIDToken builds an HS256 Identity Toolkit id token.
// Reserved claims (user_id, sub, exp, …) are set after extra and cannot be overwritten.
// When tenantID is non-empty, firebase.tenant is set (Identity Platform multi-tenancy).
func MintIdentityToolkitIDToken(projectID, localID, email string, extra map[string]any, tenantID string) (string, error) {
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
	firebase := map[string]any{
		"sign_in_provider": "password",
	}
	if t := strings.TrimSpace(tenantID); t != "" {
		firebase["tenant"] = t
	}
	claims["firebase"] = firebase
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
	claims, err := jwtutil.VerifyCompactHS256(token, identityToolkitKey())
	if err != nil {
		return "", false
	}
	aud, _ := claims["aud"].(string)
	if aud != "https://identitytoolkit.googleapis.com/google.identity.identitytoolkit.v1.IdentityToolkit" {
		return "", false
	}
	if jwtutil.ClaimExpired(claims, time.Now().UTC()) {
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
	claims, err := jwtutil.VerifyCompactHS256(token, identityToolkitKey())
	if err != nil {
		return nil, false
	}
	iss, _ := claims["iss"].(string)
	if !strings.HasPrefix(iss, "https://securetoken.google.com/") {
		return nil, false
	}
	if jwtutil.ClaimExpired(claims, time.Now().UTC()) {
		return nil, false
	}
	return claims, true
}
