// Package jwtutil provides shared RS256/HS256 JWT/JWS helpers for Noctaxris-GCP.
//
// Callers must pass only the algorithms declared in AllowedRS256 / AllowedHS256
// to ParseSigned. Fail-closed on empty input, bad signatures, and claim parse errors.
package jwtutil

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

// AllowedRS256 is the only signature algorithm accepted for RS256 verify.
var AllowedRS256 = []jose.SignatureAlgorithm{jose.RS256}

// AllowedHS256 is the only signature algorithm accepted for HS256 verify.
var AllowedHS256 = []jose.SignatureAlgorithm{jose.HS256}

// ParseJWKS unmarshals a JWKS JSON document.
func ParseJWKS(jwksJSON []byte) (*jose.JSONWebKeySet, error) {
	if len(jwksJSON) == 0 {
		return nil, fmt.Errorf("jwks: empty")
	}
	var jwks jose.JSONWebKeySet
	if err := json.Unmarshal(jwksJSON, &jwks); err != nil {
		return nil, fmt.Errorf("jwks: parse: %w", err)
	}
	if len(jwks.Keys) == 0 {
		return nil, fmt.Errorf("jwks: no keys")
	}
	return &jwks, nil
}

// VerifyCompactRS256 parses a compact JWS with RS256 only, verifies against JWKS JSON,
// and returns the payload as a generic claims map. Fail-closed on any error.
func VerifyCompactRS256(token string, jwksJSON []byte) (map[string]any, error) {
	if token == "" {
		return nil, fmt.Errorf("jwt: empty token")
	}
	jwks, err := ParseJWKS(jwksJSON)
	if err != nil {
		return nil, err
	}
	jws, err := jose.ParseSigned(token, AllowedRS256)
	if err != nil {
		return nil, fmt.Errorf("jwt: parse: %w", err)
	}
	payload, err := jws.Verify(jwks)
	if err != nil {
		return nil, fmt.Errorf("jwt: verify: %w", err)
	}
	return unmarshalClaims(payload)
}

// SignRS256 signs payload as a compact JWS (RS256) with kid in the protected header.
func SignRS256(payload []byte, key *rsa.PrivateKey, kid string) (string, error) {
	if key == nil {
		return "", fmt.Errorf("jwt: nil signing key")
	}
	if kid == "" {
		return "", fmt.Errorf("jwt: kid required")
	}
	jwk := jose.JSONWebKey{
		Key:       key,
		KeyID:     kid,
		Algorithm: string(jose.RS256),
		Use:       "sig",
	}
	opts := (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", kid)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: &jwk}, opts)
	if err != nil {
		return "", fmt.Errorf("jwt: signer: %w", err)
	}
	obj, err := signer.Sign(payload)
	if err != nil {
		return "", fmt.Errorf("jwt: sign: %w", err)
	}
	compact, err := obj.CompactSerialize()
	if err != nil {
		return "", fmt.Errorf("jwt: serialize: %w", err)
	}
	return compact, nil
}

// SignHS256 signs payload as a compact JWS (HS256) with the given symmetric key.
func SignHS256(payload []byte, key []byte) (string, error) {
	if len(key) == 0 {
		return "", fmt.Errorf("jwt: empty hmac key")
	}
	opts := (&jose.SignerOptions{}).WithType("JWT")
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.HS256, Key: key}, opts)
	if err != nil {
		return "", fmt.Errorf("jwt: signer: %w", err)
	}
	obj, err := signer.Sign(payload)
	if err != nil {
		return "", fmt.Errorf("jwt: sign: %w", err)
	}
	compact, err := obj.CompactSerialize()
	if err != nil {
		return "", fmt.Errorf("jwt: serialize: %w", err)
	}
	return compact, nil
}

// VerifyCompactHS256 parses a compact JWS with HS256 only, verifies against key,
// and returns the payload as a generic claims map. Fail-closed on any error.
func VerifyCompactHS256(token string, key []byte) (map[string]any, error) {
	if token == "" {
		return nil, fmt.Errorf("jwt: empty token")
	}
	if len(key) == 0 {
		return nil, fmt.Errorf("jwt: empty hmac key")
	}
	jws, err := jose.ParseSigned(token, AllowedHS256)
	if err != nil {
		return nil, fmt.Errorf("jwt: parse: %w", err)
	}
	payload, err := jws.Verify(key)
	if err != nil {
		return nil, fmt.Errorf("jwt: verify: %w", err)
	}
	return unmarshalClaims(payload)
}

func unmarshalClaims(payload []byte) (map[string]any, error) {
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("jwt: claims: %w", err)
	}
	return claims, nil
}

// PublicJWK builds a public JWKS key entry for an RSA signing key.
func PublicJWK(pub *rsa.PublicKey, kid string) jose.JSONWebKey {
	return jose.JSONWebKey{
		Key:       pub,
		KeyID:     kid,
		Algorithm: string(jose.RS256),
		Use:       "sig",
	}
}

// MarshalJWKS encodes a single public key as a JWKS document.
func MarshalJWKS(pub *rsa.PublicKey, kid string) ([]byte, error) {
	set := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{PublicJWK(pub, kid)}}
	return json.Marshal(set)
}

// DecodeCompactClaimsUnverified returns the payload claims of a compact JWT
// without verifying the signature. Used only for lab theatre attribute mapping
// when STS verify is off. Fail closed on non-compact or bad payload JSON.
func DecodeCompactClaimsUnverified(token string) (map[string]any, error) {
	token = strings.TrimSpace(token)
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("jwt: not a compact JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("jwt: payload decode: %w", err)
	}
	return unmarshalClaims(payload)
}

// ClaimString returns a string claim or empty.
func ClaimString(claims map[string]any, key string) string {
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

// ClaimExpired reports whether exp (unix seconds) is in the past relative to now.
// Missing or non-numeric exp is treated as expired (fail closed).
func ClaimExpired(claims map[string]any, now time.Time) bool {
	exp, ok := claimUnixSeconds(claims, "exp")
	if !ok {
		return true
	}
	return now.UTC().Unix() >= exp
}

// ClaimNotYetValid reports whether nbf (unix seconds) is in the future relative to now.
// Missing nbf is treated as valid (claim optional). Non-numeric nbf fails closed.
func ClaimNotYetValid(claims map[string]any, now time.Time) bool {
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
