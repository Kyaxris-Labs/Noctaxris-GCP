package server

import (
	"net/http"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/bigtable"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/compute"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/dataflow"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/dns"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/memorystore"
)

// registerComputeData mounts Compute Engine (incl. VPC/firewall), Bigtable Admin,
// Memorystore Redis, Cloud DNS, and Dataflow REST.
func (s *Server) registerComputeData() {
	principalFrom := func(r *http.Request) (authn.Principal, bool) {
		return PrincipalFromContext(r.Context())
	}

	ce := &compute.Service{Store: s.store, Authz: s.authz}
	ce.Mount(s.mux, principalFrom)

	bt := &bigtable.Service{Store: s.store, Authz: s.authz}
	bt.Mount(s.mux, principalFrom)

	ms := &memorystore.Service{Store: s.store, Authz: s.authz}
	ms.Mount(s.mux, principalFrom)

	dnsSvc := &dns.Service{Store: s.store, Authz: s.authz}
	dnsSvc.Mount(s.mux, principalFrom)

	df := &dataflow.Service{Store: s.store, Authz: s.authz}
	df.Mount(s.mux, principalFrom)
}
