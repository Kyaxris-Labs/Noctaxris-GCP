package iam

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/httpegress"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// EnvSTSVerify enables fail-closed OIDC JWT verify for STS when the WIF provider
// has a non-empty issuerUri. Default (unset) keeps subject_token theatre.
const EnvSTSVerify = "NOCTAXRIS_GCP_STS_VERIFY"

func stsVerifyEnabled() bool {
	v := strings.TrimSpace(os.Getenv(EnvSTSVerify))
	return v == "1" || strings.EqualFold(v, "true")
}

// stsOIDCShouldVerify reports whether this exchange must verify the subject JWT.
func stsOIDCShouldVerify(issuerURI string) bool {
	return stsVerifyEnabled() && strings.TrimSpace(issuerURI) != ""
}

// FetchURL GETs a URL after httpegress.Validate (fail-closed). Tests may set
// Handler.STSFetch to serve discovery/JWKS without dialing.
func (h *Handler) fetchSTSURL(rawURL string) ([]byte, error) {
	rawURL = strings.TrimSpace(rawURL)
	if err := httpegress.Validate(rawURL); err != nil {
		return nil, fmt.Errorf("sts oidc: %w", err)
	}
	if h != nil && h.STSFetch != nil {
		return h.STSFetch(rawURL)
	}
	client := httpegress.Client(5 * time.Second)
	resp, err := client.Get(rawURL)
	if err != nil {
		return nil, fmt.Errorf("sts oidc: fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sts oidc: fetch status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("sts oidc: read: %w", err)
	}
	return body, nil
}

func (h *Handler) fetchOIDCJWKS(issuerURI string) ([]byte, error) {
	issuer := strings.TrimRight(strings.TrimSpace(issuerURI), "/")
	if issuer == "" {
		return nil, fmt.Errorf("sts oidc: issuerUri required")
	}
	discoveryURL := issuer + "/.well-known/openid-configuration"
	jwksDirect := issuer + "/.well-known/jwks.json"

	if body, err := h.fetchSTSURL(discoveryURL); err == nil {
		var disc struct {
			JWKSURI string `json:"jwks_uri"`
		}
		if json.Unmarshal(body, &disc) == nil && strings.TrimSpace(disc.JWKSURI) != "" {
			return h.fetchSTSURL(strings.TrimSpace(disc.JWKSURI))
		}
	}
	return h.fetchSTSURL(jwksDirect)
}

// verifyOIDCSubjectToken verifies RS256 JWT signature + iss/aud/exp basics.
// Returns the sanitized subject claim for the WIF principal.
func (h *Handler) verifyOIDCSubjectToken(subjectToken string, prov store.WorkloadIdentityPoolProvider) (string, error) {
	jwksJSON, err := h.fetchOIDCJWKS(prov.IssuerURI)
	if err != nil {
		return "", err
	}
	claims, err := verifyCompactRS256(subjectToken, jwksJSON)
	if err != nil {
		return "", err
	}
	now := h.now()
	if claimExpired(claims, now) {
		return "", fmt.Errorf("sts oidc: token expired")
	}
	if claimNotYetValid(claims, now) {
		return "", fmt.Errorf("sts oidc: token not yet valid")
	}
	iss := claimString(claims, "iss")
	wantIss := strings.TrimRight(strings.TrimSpace(prov.IssuerURI), "/")
	gotIss := strings.TrimRight(strings.TrimSpace(iss), "/")
	if gotIss == "" || !strings.EqualFold(gotIss, wantIss) {
		return "", fmt.Errorf("sts oidc: iss mismatch")
	}
	if !audienceOK(claims, prov.Name) {
		return "", fmt.Errorf("sts oidc: aud mismatch")
	}
	sub := claimString(claims, "sub")
	if strings.TrimSpace(sub) == "" {
		return "", fmt.Errorf("sts oidc: sub required")
	}
	return labSubjectFromToken(sub), nil
}

func audienceOK(claims map[string]any, providerName string) bool {
	allowed := map[string]struct{}{
		providerName:                         {},
		"//iam.googleapis.com/" + providerName: {},
	}
	auds := claimAudienceList(claims)
	if len(auds) == 0 {
		return false
	}
	for _, a := range auds {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if _, ok := allowed[a]; ok {
			return true
		}
	}
	return false
}

func claimAudienceList(claims map[string]any) []string {
	if claims == nil {
		return nil
	}
	v, ok := claims["aud"]
	if !ok || v == nil {
		return nil
	}
	switch t := v.(type) {
	case string:
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func claimString(claims map[string]any, key string) string {
	if claims == nil {
		return ""
	}
	v, ok := claims[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return fmt.Sprintf("%.0f", t)
	default:
		return fmt.Sprint(t)
	}
}

func claimExpired(claims map[string]any, now time.Time) bool {
	exp, ok := claimUnixSeconds(claims, "exp")
	if !ok {
		return true
	}
	return now.UTC().Unix() >= exp
}

func claimNotYetValid(claims map[string]any, now time.Time) bool {
	if claims == nil {
		return false
	}
	if _, present := claims["nbf"]; !present {
		return false
	}
	nbf, ok := claimUnixSeconds(claims, "nbf")
	if !ok {
		return true
	}
	return now.UTC().Unix() < nbf
}

func claimUnixSeconds(claims map[string]any, key string) (int64, bool) {
	if claims == nil {
		return 0, false
	}
	v, ok := claims[key]
	if !ok {
		return 0, false
	}
	switch t := v.(type) {
	case float64:
		return int64(t), true
	case json.Number:
		n, err := t.Int64()
		if err != nil {
			return 0, false
		}
		return n, true
	case int64:
		return t, true
	case int:
		return int64(t), true
	default:
		return 0, false
	}
}

type jwkRSA struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type jwksDoc struct {
	Keys []jwkRSA `json:"keys"`
}

func verifyCompactRS256(token string, jwksJSON []byte) (map[string]any, error) {
	token = strings.TrimSpace(token)
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("sts oidc: not a compact JWT")
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("sts oidc: header decode: %w", err)
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return nil, fmt.Errorf("sts oidc: header json: %w", err)
	}
	if !strings.EqualFold(header.Alg, "RS256") {
		return nil, fmt.Errorf("sts oidc: alg must be RS256")
	}
	var doc jwksDoc
	if err := json.Unmarshal(jwksJSON, &doc); err != nil {
		return nil, fmt.Errorf("sts oidc: jwks: %w", err)
	}
	if len(doc.Keys) == 0 {
		return nil, fmt.Errorf("sts oidc: jwks empty")
	}
	var pub *rsa.PublicKey
	for _, k := range doc.Keys {
		if !strings.EqualFold(k.Kty, "RSA") {
			continue
		}
		if header.Kid != "" && k.Kid != "" && k.Kid != header.Kid {
			continue
		}
		if k.Alg != "" && !strings.EqualFold(k.Alg, "RS256") {
			continue
		}
		pk, err := rsaPublicFromJWK(k.N, k.E)
		if err != nil {
			continue
		}
		pub = pk
		break
	}
	if pub == nil {
		return nil, fmt.Errorf("sts oidc: no matching JWKS key")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("sts oidc: sig decode: %w", err)
	}
	signingInput := parts[0] + "." + parts[1]
	sum := sha256.Sum256([]byte(signingInput))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig); err != nil {
		return nil, fmt.Errorf("sts oidc: signature: %w", err)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("sts oidc: payload decode: %w", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("sts oidc: claims: %w", err)
	}
	return claims, nil
}

func rsaPublicFromJWK(nB64, eB64 string) (*rsa.PublicKey, error) {
	nb, err := base64.RawURLEncoding.DecodeString(nB64)
	if err != nil {
		return nil, err
	}
	eb, err := base64.RawURLEncoding.DecodeString(eB64)
	if err != nil {
		return nil, err
	}
	if len(nb) == 0 || len(eb) == 0 {
		return nil, fmt.Errorf("empty modulus or exponent")
	}
	n := new(big.Int).SetBytes(nb)
	e := 0
	for _, b := range eb {
		e = e<<8 + int(b)
	}
	if e < 2 {
		return nil, fmt.Errorf("invalid exponent")
	}
	return &rsa.PublicKey{N: n, E: e}, nil
}
