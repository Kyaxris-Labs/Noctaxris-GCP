package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/jwtutil"
)

// OIDCLabPath is the lab OIDC issuer mount (discovery + JWKS only; no token mint).
const OIDCLabPath = "/_noctaxris-gcp/oidc-lab"

// LabOIDCKid is the stable key id published in JWKS.
const LabOIDCKid = jwtutil.LabOIDCKid

// OIDCLabIssuerURL returns the issuer URI for a lab API base (scheme + host, no path).
func OIDCLabIssuerURL(apiBase string) string {
	base := strings.TrimRight(strings.TrimSpace(apiBase), "/")
	return base + OIDCLabPath
}

// SignOIDCLabJWT signs claims with the stable oidc-lab RSA key (RS256 via jose).
func SignOIDCLabJWT(claims map[string]any) (string, error) {
	return jwtutil.SignLabOIDCRS256(claims)
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
	raw, err := jwtutil.MarshalLabOIDCJWKS()
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "oidc lab key")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}
