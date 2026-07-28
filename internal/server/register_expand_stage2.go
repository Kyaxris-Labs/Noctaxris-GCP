package server

import (
	"net/http"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/appengine"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/artifactregistry"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudbuild"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/spanner"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/workflows"
)

// registerExpandStage2 mounts Stage 2 new services.
// CRM orgs/folders are wired through registerIdentity (resourcemanager.Mount).
func (s *Server) registerExpandStage2() {
	principalFrom := func(r *http.Request) (authn.Principal, bool) {
		return PrincipalFromContext(r.Context())
	}

	ar := &artifactregistry.Service{Store: s.store, Authz: s.authz}
	ar.Mount(s.mux, principalFrom)

	cb := &cloudbuild.Service{Store: s.store, Authz: s.authz}
	cb.Mount(s.mux, principalFrom)

	wf := &workflows.Service{Store: s.store, Authz: s.authz}
	wf.Mount(s.mux, principalFrom)

	sp := &spanner.Service{Store: s.store, Authz: s.authz}
	sp.Mount(s.mux, principalFrom)

	ae := &appengine.Service{Store: s.store, Authz: s.authz}
	ae.Mount(s.mux, principalFrom)
}
