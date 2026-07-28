package server

import (
	"context"
	"net/http"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/gcs"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/pubsub"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/secretmanager"
)

// registerData mounts Wave 1 DATA services: Cloud Storage, Pub/Sub, Secret Manager.
// Requires s.grpc from registerIdentity / newGRPCServer.
func (s *Server) registerData() {
	if s.grpc == nil {
		s.grpc = s.newGRPCServer()
	}

	httpPrincipal := func(r *http.Request) (authn.Principal, bool) {
		return PrincipalFromContext(r.Context())
	}
	// Prefer interceptor-injected principal; fall back to metadata Bearer if needed.
	grpcPrincipal := pubsub.PrincipalFromAuthn(s.authn, func(ctx context.Context) (authn.Principal, bool) {
		return PrincipalFromContext(ctx)
	})

	gcsHandler := &gcs.Handler{
		Store:          s.store,
		Authz:          s.authz,
		Principal:      httpPrincipal,
		DefaultProject: s.cfg.ProjectID,
	}
	gcsHandler.Register(s.mux)

	ps := &pubsub.Service{
		Store:     s.store,
		Authz:     s.authz,
		Principal: grpcPrincipal,
	}
	ps.Register(s.grpc)

	sm := &secretmanager.Service{
		Store:         s.store,
		Authz:         s.authz,
		HTTPPrincipal: httpPrincipal,
		GRPCPrincipal: func(ctx context.Context) (authn.Principal, error) {
			return grpcPrincipal(ctx)
		},
		DefaultProject: s.cfg.ProjectID,
	}
	sm.RegisterREST(s.mux)
	sm.RegisterGRPC(s.grpc)
}

// RegisterData is exported for coordinators that wire registration outside New.
func (s *Server) RegisterData() { s.registerData() }
