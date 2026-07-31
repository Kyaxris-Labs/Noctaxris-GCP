package server

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"strings"
	"sync"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
)

// OIDCLabPath is the lab OIDC issuer mount (discovery + JWKS only; no token mint).
const OIDCLabPath = "/_noctaxris-gcp/oidc-lab"

// LabOIDCKid is the stable key id published in JWKS.
const LabOIDCKid = "noctaxris-gcp-oidc-lab"

// labOIDCPrivateKeyPEM is a fixed in-process RSA key for the oidc-lab issuer.
const labOIDCPrivateKeyPEM = `-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEAyZPcBwc0jZyoaEvE3Irp0Aaismc+PzVkz8J0viX7jIjSqA4r
MRy68gJlduMnOcxKFre/SJodEvDvUi/yl7EmcC2AAtxeRnJSsHmKcoSs5nwSg2YO
2t3rzj2vxVrzGsUkDHlYhpwwtbd1pO0w/MmTBGgZz/VjmmPgjcTqxY+d3V0PwnCK
FxvaXNuc0edaBa5y2W1OTtd8sDO1YGMgPQP2aiGEiqqKIkChY3/zD3DZtUzCLiA6
LW8U3T9pMs4uc87CnnX/Y31go6Sq+oL8ZLeG+jrbPqzx2/QqMEDKZhCRT0lrAAEN
ezqXy9QhodyQlLbC+NOAxxqra9Rlth6YvoUeaQIDAQABAoIBABLxd8BwqhIoJ4q2
8CGlAP22hJdNF4RGJsVVf2DCiLC3cksucFsAWbCA5CXM3rSnoPYglMkkXoNsq/jm
c57+HArNDoDLp8++p6GuUldFEPXksV1de8Y6tmcef0RbMd8RVBFuB+aNNWX7qdfI
HeNA/YrGTlgE4LQzcE4HtDkWWt4LWXRqV0oBDh3yA+8YDqEY+pyXR5506YFQJ4JM
y3/NB7wayarKeOMvyZQ6REfwsdG6o6yYj8/85flDjRS9iaM9Y9XQ8CCZZJrB8b/A
3YWZEyy6wKgHI1IC1Q5OLTPFBxcKT999/s+aKGBFmPO5yEy7hQ7d8FeTUPYRxIYc
Rq6LiKcCgYEA+KTuchwcKEh/OxGTQ6XOKSERVgsJA/Tt+gIxKHBlor9QB9ivUzFn
HTytjm0YjOdEqorBwgaXLEovv311ZSOC3FWEUwNc3R8fSc9d7j1Svx+DPa7G1Bl4
ae96rzXiAo/wmCsIb/Bn43olF0O6kL9L7TiLIpG0+UND4I0hyfZGb1sCgYEAz4p5
0EAYbfIFhvEQuhHxFDit0akk11Aw4UO0bqPsf2rI0rTfDY4RJgt90bPWoxCksovI
wFRe2unGP9j/tTnApjc7e83AGWD1gSG5qEM0VnEe/RyE9FGeGk7CGEDKumOmGVRg
m9AvR8dL9v3FuJhmB2ZcbAderTeBSgXy0dZ8eIsCgYEAsi/QSapna174/uXLeXE7
WzI9cEIcRd+jI8WqYOabj5Q20Eiy7JW85bD0V9tK+r9J8EXcMSXz9GN98GcCWGao
gyot2CfSxwxkqcqX8AG2aQ02SmAUUS+noZNjgmjE/T0WGJbORxor+VMxfYimDNFq
oighXba50OAppqS9kDSTqX0CgYEAitgVTmDS9xrm37P+gLzoD6MrhgwmfXVEfi+R
UkOQQF3sJCqk3qigiFc/wT8S5NyJknk5wJGxM7sZyjUePNt6Krjgrp6jWVcoZ09s
qUjshrf/B05BFEJWBzuRVjBib/eic2ejihnox5hpFcAIusoZ1/F++zai/DcZ46+/
FurrMqkCgYBo/Po7fjpyVjYwAOue8TOByzzu2+8SfQnreaFFXM9S1g3l6uALemKH
qHdxLP3dGLbHUK9c/f3XVy2EWnjw24Dk7lmVdNJPr/5gg/jCi4hzp7RTGImZ5rmy
oF3IzvPPtfypNh/Ex7VO8n4XYEzR1T3emupnFYE3mO6N8ImlOpYFjQ==
-----END RSA PRIVATE KEY-----`

var (
	labOIDCKeyOnce sync.Once
	labOIDCKey     *rsa.PrivateKey
	labOIDCKeyErr  error
)

func labOIDCPrivateKey() (*rsa.PrivateKey, error) {
	labOIDCKeyOnce.Do(func() {
		block, _ := pem.Decode([]byte(labOIDCPrivateKeyPEM))
		if block == nil {
			labOIDCKeyErr = x509.ErrUnsupportedAlgorithm
			return
		}
		key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			labOIDCKeyErr = err
			return
		}
		labOIDCKey = key
	})
	return labOIDCKey, labOIDCKeyErr
}

// OIDCLabIssuerURL returns the issuer URI for a lab API base (scheme + host, no path).
func OIDCLabIssuerURL(apiBase string) string {
	base := strings.TrimRight(strings.TrimSpace(apiBase), "/")
	return base + OIDCLabPath
}

// SignOIDCLabJWT signs claims with the stable oidc-lab RSA key (RS256).
func SignOIDCLabJWT(claims map[string]any) (string, error) {
	key, err := labOIDCPrivateKey()
	if err != nil {
		return "", err
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT","kid":"` + LabOIDCKid + `"}`))
	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	signingInput := header + "." + payload
	sum := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func (s *Server) registerOIDCLab() {
	discovery := OIDCLabPath + "/.well-known/openid-configuration"
	jwks := OIDCLabPath + "/.well-known/jwks.json"
	s.mux.HandleFunc("GET "+discovery, s.handleOIDCLabDiscovery)
	s.mux.HandleFunc("GET "+jwks, s.handleOIDCLabJWKS)
}

func oidcLabIssuerFromRequest(r *http.Request) string {
	host := strings.TrimSpace(r.Host)
	if host == "" {
		host = "127.0.0.1:4588"
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + host + OIDCLabPath
}

func (s *Server) handleOIDCLabDiscovery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		gcperrors.WriteREST(w, http.StatusMethodNotAllowed, gcperrors.StatusInvalidArgument, "method not allowed")
		return
	}
	issuer := oidcLabIssuerFromRequest(r)
	jwksURI := issuer + "/.well-known/jwks.json"
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"issuer":   issuer,
		"jwks_uri": jwksURI,
	})
}

func (s *Server) handleOIDCLabJWKS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		gcperrors.WriteREST(w, http.StatusMethodNotAllowed, gcperrors.StatusInvalidArgument, "method not allowed")
		return
	}
	key, err := labOIDCPrivateKey()
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "oidc lab key")
		return
	}
	pub := &key.PublicKey
	n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
	eb := big.NewInt(int64(pub.E)).Bytes()
	e := base64.RawURLEncoding.EncodeToString(eb)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"keys": []map[string]string{{
			"kty": "RSA",
			"kid": LabOIDCKid,
			"alg": "RS256",
			"use": "sig",
			"n":   n,
			"e":   e,
		}},
	})
}
