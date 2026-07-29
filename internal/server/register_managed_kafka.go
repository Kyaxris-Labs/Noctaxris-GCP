package server

import (
	"net/http"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/managedkafka"
)

func (s *Server) registerManagedKafka() {
	principalFrom := func(r *http.Request) (authn.Principal, bool) {
		return PrincipalFromContext(r.Context())
	}
	mk := &managedkafka.Service{
		Store:          s.store,
		Authz:          s.authz,
		DockerHost:     s.cfg.DockerHost,
		DockerCertPath: s.cfg.DockerTLSCertPath,
	}
	mk.Mount(s.mux, principalFrom)
}
