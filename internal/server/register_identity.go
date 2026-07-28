package server

import (
	"net/http"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/iam"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/resourcemanager"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/serviceusage"
)

// registerIdentity mounts Cloud Resource Manager, IAM Admin, and Service Usage
// REST handlers and installs gRPC Bearer auth interceptors on s.grpc.
func (s *Server) registerIdentity() {
	s.grpc = s.newGRPCServer()

	principal := func(r *http.Request) (authn.Principal, bool) {
		return PrincipalFromContext(r.Context())
	}

	crm := &resourcemanager.Handler{
		Store:     s.store,
		Authz:     s.authz,
		Principal: principal,
	}
	crm.Mount(s.mux)

	iamH := &iam.Handler{
		Store:     s.store,
		Authz:     s.authz,
		Principal: principal,
	}
	iamH.Mount(s.mux)

	su := &serviceusage.Handler{
		Store:     s.store,
		Authz:     s.authz,
		Principal: principal,
	}
	su.Mount(s.mux)
}
