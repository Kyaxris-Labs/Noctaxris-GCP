package authn

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// LabIdentityToolkitUID reports the localId from an unsigned Identity Toolkit id token.
func LabIdentityToolkitUID(token string) (string, bool) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
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
	iss, _ := claims["iss"].(string)
	if !strings.HasPrefix(iss, "https://securetoken.google.com/") {
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
