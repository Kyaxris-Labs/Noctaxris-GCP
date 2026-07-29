package server

import (
	"net/http"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudasset"
)

// registerCloudAsset mounts Cloud Asset Inventory search/list/export lite REST.
// Wire from server.New as: s.registerCloudAsset()
func (s *Server) registerCloudAsset() {
	principalFrom := func(r *http.Request) (authn.Principal, bool) {
		return PrincipalFromContext(r.Context())
	}
	ca := &cloudasset.Service{Store: s.store, Authz: s.authz}
	ca.Mount(s.mux, principalFrom)
}

// RegisterCloudAsset is the exported wire-up alias for tests / explicit mounts.
func (s *Server) RegisterCloudAsset() { s.registerCloudAsset() }
