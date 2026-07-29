package server

import (
	"net/http"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/accesscontextmanager"
)

// registerAccessContextManager mounts Access Context Manager (VPC SC perimeter lite) REST.
// Wire from server.New as: s.registerAccessContextManager()
func (s *Server) registerAccessContextManager() {
	principalFrom := func(r *http.Request) (authn.Principal, bool) {
		return PrincipalFromContext(r.Context())
	}
	acm := &accesscontextmanager.Service{Store: s.store, Authz: s.authz}
	acm.Mount(s.mux, principalFrom)
}

// RegisterAccessContextManager is the exported wire-up alias for tests / explicit mounts.
func (s *Server) RegisterAccessContextManager() { s.registerAccessContextManager() }
