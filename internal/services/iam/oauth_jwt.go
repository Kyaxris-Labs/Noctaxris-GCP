package iam

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// GrantTypeJWTBearer is the OAuth 2.0 JWT bearer assertion grant type.
const GrantTypeJWTBearer = "urn:ietf:params:oauth:grant-type:jwt-bearer"

const (
	oauthAccessTokenTTL = time.Hour
	oauthMaxClockSkew   = 5 * time.Minute
	oauthMaxJWTLIfe     = time.Hour
)

// MountOAuth registers Google-shaped OAuth token endpoints for SA JWT bearer grants.
// POST /token and POST /oauth2/token are public (key possession authenticates).
// VPC-SC is not applied (matches real oauth2 token URI key-possession mint).
func (h *Handler) MountOAuth(mux *http.ServeMux) {
	mux.HandleFunc("POST /token", h.oauthJWTBearerToken)
	mux.HandleFunc("POST /oauth2/token", h.oauthJWTBearerToken)
}

func (h *Handler) oauthJWTBearerToken(w http.ResponseWriter, r *http.Request) {
	grantType, assertion, _ := parseOAuthTokenForm(r)
	if grantType != GrantTypeJWTBearer {
		gcperrors.InvalidArgument(w, "grant_type must be "+GrantTypeJWTBearer)
		return
	}
	if strings.TrimSpace(assertion) == "" {
		gcperrors.InvalidArgument(w, "assertion is required")
		return
	}

	saEmail, err := h.verifySAJWTBearer(assertion)
	if err != nil {
		gcperrors.Unauthenticated(w, "invalid_grant")
		return
	}
	sa, ok, err := h.Store.GetServiceAccount(saEmail)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok || sa.DeletedAt != "" || sa.Disabled {
		gcperrors.Unauthenticated(w, "invalid_grant")
		return
	}

	token := newAccessToken()
	expire := h.now().Add(oauthAccessTokenTTL)
	if err := h.Store.PutAccessToken(authn.HashToken(token), sa.Email, expire); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": token,
		"token_type":   "Bearer",
		"expires_in":   int(oauthAccessTokenTTL / time.Second),
	})
}

func parseOAuthTokenForm(r *http.Request) (grantType, assertion, scope string) {
	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			return "", "", ""
		}
		var req struct {
			GrantType      string `json:"grant_type"`
			GrantTypeCamel string `json:"grantType"`
			Assertion      string `json:"assertion"`
			Scope          string `json:"scope"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			return "", "", ""
		}
		return firstNonEmpty(req.GrantType, req.GrantTypeCamel), strings.TrimSpace(req.Assertion), strings.TrimSpace(req.Scope)
	}
	_ = r.ParseForm()
	vals := r.Form
	if vals == nil {
		raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		vals, _ = url.ParseQuery(string(raw))
	}
	return vals.Get("grant_type"), strings.TrimSpace(vals.Get("assertion")), strings.TrimSpace(vals.Get("scope"))
}

func (h *Handler) verifySAJWTBearer(assertion string) (saEmail string, err error) {
	header, claims, signingInput, sig, err := parseCompactJWT(assertion)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(header.Alg, "RS256") {
		return "", fmt.Errorf("oauth jwt: alg must be RS256")
	}
	iss := claimString(claims, "iss")
	sub := claimString(claims, "sub")
	if iss == "" || sub == "" || iss != sub {
		return "", fmt.Errorf("oauth jwt: iss and sub must equal an SA email")
	}
	sa, ok, err := h.Store.GetServiceAccount(iss)
	if err != nil {
		return "", err
	}
	if !ok || sa.DeletedAt != "" || sa.Disabled || sa.Email != iss {
		return "", fmt.Errorf("oauth jwt: unknown service account")
	}
	if !oauthJWTAudienceOK(claims) {
		return "", fmt.Errorf("oauth jwt: aud not allowed")
	}
	now := h.now().UTC()
	if err := validateOAuthJWTTime(claims, now); err != nil {
		return "", err
	}

	kid := strings.TrimSpace(header.Kid)
	if kid == "" {
		kid = strings.TrimSpace(claimString(claims, "kid"))
	}
	if kid == "" {
		kid = strings.TrimSpace(claimString(claims, "private_key_id"))
	}

	keys, err := h.Store.ListServiceAccountKeys(sa.Email)
	if err != nil {
		return "", err
	}
	if len(keys) == 0 {
		return "", fmt.Errorf("oauth jwt: no keys")
	}

	ordered := orderSAKeysForJWT(keys, kid)
	for _, k := range ordered {
		pub, ok := h.rsaPublicFromSealedKey(k)
		if !ok {
			continue
		}
		sum := sha256.Sum256([]byte(signingInput))
		if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig); err == nil {
			return sa.Email, nil
		}
	}
	return "", fmt.Errorf("oauth jwt: signature invalid")
}

type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	Typ string `json:"typ"`
}

func parseCompactJWT(token string) (jwtHeader, map[string]any, string, []byte, error) {
	token = strings.TrimSpace(token)
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return jwtHeader{}, nil, "", nil, fmt.Errorf("oauth jwt: not a compact JWT")
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return jwtHeader{}, nil, "", nil, fmt.Errorf("oauth jwt: header decode: %w", err)
	}
	var header jwtHeader
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return jwtHeader{}, nil, "", nil, fmt.Errorf("oauth jwt: header json: %w", err)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return jwtHeader{}, nil, "", nil, fmt.Errorf("oauth jwt: payload decode: %w", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return jwtHeader{}, nil, "", nil, fmt.Errorf("oauth jwt: claims: %w", err)
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return jwtHeader{}, nil, "", nil, fmt.Errorf("oauth jwt: sig decode: %w", err)
	}
	return header, claims, parts[0] + "." + parts[1], sig, nil
}

func validateOAuthJWTTime(claims map[string]any, now time.Time) error {
	exp, ok := claimUnixSeconds(claims, "exp")
	if !ok {
		return fmt.Errorf("oauth jwt: exp required")
	}
	nowUnix := now.Unix()
	if nowUnix >= exp {
		return fmt.Errorf("oauth jwt: expired")
	}
	if exp > nowUnix+int64(oauthMaxJWTLIfe/time.Second)+int64(oauthMaxClockSkew/time.Second) {
		return fmt.Errorf("oauth jwt: exp too far")
	}
	if iat, ok := claimUnixSeconds(claims, "iat"); ok {
		if iat > nowUnix+int64(oauthMaxClockSkew/time.Second) {
			return fmt.Errorf("oauth jwt: iat far future")
		}
		if exp-iat > int64(oauthMaxJWTLIfe/time.Second)+int64(oauthMaxClockSkew/time.Second) {
			return fmt.Errorf("oauth jwt: lifetime too long")
		}
	}
	if claimNotYetValid(claims, now) {
		return fmt.Errorf("oauth jwt: not yet valid")
	}
	return nil
}

func oauthJWTAudienceOK(claims map[string]any) bool {
	for _, aud := range claimAudienceList(claims) {
		if oauthAudienceAllowed(aud) {
			return true
		}
	}
	return false
}

func oauthAudienceAllowed(aud string) bool {
	aud = strings.TrimSpace(aud)
	switch aud {
	case "https://oauth2.googleapis.com/token",
		"https://www.googleapis.com/oauth2/v4/token",
		"https://accounts.google.com/o/oauth2/token",
		"/token",
		"/oauth2/token":
		return true
	}
	u, err := url.Parse(aud)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	p := path.Clean("/" + strings.TrimPrefix(u.Path, "/"))
	if p != "/token" && p != "/oauth2/token" {
		return false
	}
	// Lab token URIs only (loopback / engine bridge), not arbitrary hosts with /token.
	return oauthLabAudienceHost(u.Hostname())
}

func oauthLabAudienceHost(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "127.0.0.1", "localhost", "::1", "host.docker.internal":
		return true
	default:
		return false
	}
}

func orderSAKeysForJWT(keys []store.ServiceAccountKey, preferKeyID string) []store.ServiceAccountKey {
	if preferKeyID == "" || len(keys) < 2 {
		return keys
	}
	out := make([]store.ServiceAccountKey, 0, len(keys))
	var rest []store.ServiceAccountKey
	for _, k := range keys {
		if saKeyIDFromName(k.Name) == preferKeyID {
			out = append(out, k)
			continue
		}
		rest = append(rest, k)
	}
	return append(out, rest...)
}

func saKeyIDFromName(name string) string {
	idx := strings.LastIndex(name, "/keys/")
	if idx < 0 {
		return ""
	}
	return name[idx+len("/keys/"):]
}

func (h *Handler) rsaPublicFromSealedKey(k store.ServiceAccountKey) (*rsa.PublicKey, bool) {
	plain, err := h.Store.Unseal(k.PrivateKeyData)
	if err != nil {
		return nil, false
	}
	var cred struct {
		PrivateKey   string `json:"private_key"`
		PrivateKeyID string `json:"private_key_id"`
	}
	if err := json.Unmarshal(plain, &cred); err != nil {
		return nil, false
	}
	pub, err := rsaPublicFromPKCS8PEM(cred.PrivateKey)
	if err != nil {
		return nil, false
	}
	return pub, true
}

func rsaPublicFromPKCS8PEM(pemStr string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("no PEM block")
	}
	if block.Type != "PRIVATE KEY" && block.Type != "RSA PRIVATE KEY" {
		return nil, fmt.Errorf("unexpected PEM type %q", block.Type)
	}
	keyAny, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		// Accept PKCS#1 for older fixtures if present.
		if pk, err2 := x509.ParsePKCS1PrivateKey(block.Bytes); err2 == nil {
			return &pk.PublicKey, nil
		}
		return nil, err
	}
	priv, ok := keyAny.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("not RSA private key")
	}
	return &priv.PublicKey, nil
}
