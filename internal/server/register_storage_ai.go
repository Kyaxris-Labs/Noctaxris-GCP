package server

import (
	"net/http"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/filestore"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/vertexai"
)

// registerStorageAI mounts Filestore and Vertex AI REST.
func (s *Server) registerStorageAI() {
	principalFrom := func(r *http.Request) (authn.Principal, bool) {
		return PrincipalFromContext(r.Context())
	}

	fs := &filestore.Service{Store: s.store, Authz: s.authz}
	fs.Mount(s.mux, principalFrom)

	vai := &vertexai.Service{Authz: s.authz}
	vai.Mount(s.mux, principalFrom)
}
