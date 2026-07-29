package server

import (
	"net/http"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/orgpolicy"
)

// registerOrgPolicy mounts Organization Policy API v2 REST theatre.
// Wire from Server.New after registerIdentity (or alongside identity):
//
//	s.registerOrgPolicy()
func (s *Server) registerOrgPolicy() {
	principal := func(r *http.Request) (authn.Principal, bool) {
		return PrincipalFromContext(r.Context())
	}
	h := &orgpolicy.Handler{
		Store:     s.store,
		Authz:     s.authz,
		Principal: principal,
	}
	h.Mount(s.mux)
}
