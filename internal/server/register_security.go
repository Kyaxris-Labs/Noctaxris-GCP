package server

import (
	"net/http"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/certificatemanager"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudarmor"
)

// registerSecurity mounts Cloud Armor (Compute securityPolicies) and Certificate Manager REST.
func (s *Server) registerSecurity() {
	principalFrom := func(r *http.Request) (authn.Principal, bool) {
		return PrincipalFromContext(r.Context())
	}

	armor := &cloudarmor.Service{Store: s.store, Authz: s.authz}
	armor.Mount(s.mux, principalFrom)

	cm := &certificatemanager.Service{Store: s.store, Authz: s.authz}
	cm.Mount(s.mux, principalFrom)
}
