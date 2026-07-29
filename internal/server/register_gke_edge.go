package server

import (
	"net/http"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cdn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/gke"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/loadbalancing"
)

// registerGKEEdge mounts GKE Container API, HTTP(S) load balancing, and Cloud CDN.
func (s *Server) registerGKEEdge() {
	principalFrom := func(r *http.Request) (authn.Principal, bool) {
		return PrincipalFromContext(r.Context())
	}

	gkeSvc := &gke.Service{Store: s.store, Authz: s.authz}
	gkeSvc.Mount(s.mux, principalFrom)

	lb := &loadbalancing.Service{Store: s.store, Authz: s.authz}
	lb.Mount(s.mux, principalFrom)

	cdnSvc := &cdn.Service{Store: s.store, Authz: s.authz}
	cdnSvc.Mount(s.mux, principalFrom)
}
