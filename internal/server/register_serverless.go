package server

import (
	"net/http"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudfunctions"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudrun"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudtasks"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/scheduler"
)

// registerServerless registers Cloud Run, Cloud Functions, Cloud Scheduler, and Cloud Tasks REST.
func (s *Server) registerServerless() {
	principalFrom := func(r *http.Request) (authn.Principal, bool) {
		return PrincipalFromContext(r.Context())
	}

	runSvc := &cloudrun.Service{Store: s.store, Authz: s.authz}
	runSvc.Mount(s.mux, principalFrom)

	fnSvc := &cloudfunctions.Service{Store: s.store, Authz: s.authz}
	fnSvc.Mount(s.mux, principalFrom)

	schedSvc := &scheduler.Service{Store: s.store, Authz: s.authz}
	schedSvc.Mount(s.mux, principalFrom)

	tasksSvc := &cloudtasks.Service{Store: s.store, Authz: s.authz}
	tasksSvc.Mount(s.mux, principalFrom)
}
