package server

import (
	"net/http"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/securitycenter"
)

// registerSecurityCenter mounts Security Command Center sources/findings lite + lab inject.
// Wire from Server.New (do not leave orphaned when integrating Cut C).
func (s *Server) registerSecurityCenter() {
	principalFrom := func(r *http.Request) (authn.Principal, bool) {
		return PrincipalFromContext(r.Context())
	}
	scc := &securitycenter.Service{
		Store:         s.store,
		Authz:         s.authz,
		InjectEnabled: securitycenter.InjectEnabledFromEnv(),
	}
	scc.Mount(s.mux, principalFrom)
}
