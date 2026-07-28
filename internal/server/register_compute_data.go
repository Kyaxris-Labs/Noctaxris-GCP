package server

import (
	"context"
	"net/http"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/bigtable"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/compute"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/dataflow"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/dns"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/memorystore"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/pubsub"
)

// registerComputeData mounts Compute Engine (incl. VPC/firewall), Bigtable Admin
// (REST + Instance Admin gRPC lite), Memorystore Redis, Cloud DNS, and Dataflow REST.
func (s *Server) registerComputeData() {
	if s.grpc == nil {
		s.grpc = s.newGRPCServer()
	}

	principalFrom := func(r *http.Request) (authn.Principal, bool) {
		return PrincipalFromContext(r.Context())
	}
	grpcPrincipal := pubsub.PrincipalFromAuthn(s.authn, func(ctx context.Context) (authn.Principal, bool) {
		return PrincipalFromContext(ctx)
	})

	ce := &compute.Service{Store: s.store, Authz: s.authz}
	ce.Mount(s.mux, principalFrom)

	bt := &bigtable.Service{
		Store: s.store,
		Authz: s.authz,
		GRPCPrincipal: func(ctx context.Context) (authn.Principal, error) {
			return grpcPrincipal(ctx)
		},
	}
	bt.Mount(s.mux, principalFrom)
	bt.RegisterGRPC(s.grpc)

	ms := &memorystore.Service{Store: s.store, Authz: s.authz}
	ms.Mount(s.mux, principalFrom)

	dnsSvc := &dns.Service{Store: s.store, Authz: s.authz}
	dnsSvc.Mount(s.mux, principalFrom)

	df := &dataflow.Service{Store: s.store, Authz: s.authz}
	df.Mount(s.mux, principalFrom)
}
