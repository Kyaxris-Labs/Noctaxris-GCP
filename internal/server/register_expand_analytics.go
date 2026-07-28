package server

import (
	"net/http"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/bigquery"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/datastore"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/eventarc"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/firebaseauth"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/monitoring"
)

// registerExpandAnalytics mounts BigQuery, Firebase Auth, Monitoring, Datastore, and Eventarc.
func (s *Server) registerExpandAnalytics() {
	if s.grpc == nil {
		s.grpc = s.newGRPCServer()
	}

	principalFrom := func(r *http.Request) (authn.Principal, bool) {
		return PrincipalFromContext(r.Context())
	}

	bq := &bigquery.Service{Store: s.store, Authz: s.authz}
	bq.Mount(s.mux, principalFrom)

	fa := &firebaseauth.Service{
		Store: s.store, Authz: s.authz, DefaultProject: s.cfg.ProjectID,
	}
	fa.Mount(s.mux, principalFrom)

	mon := &monitoring.Service{Store: s.store, Authz: s.authz}
	mon.Mount(s.mux, principalFrom)

	ds := &datastore.Service{
		Store:         s.store,
		Authn:         s.authn,
		Authz:         s.authz,
		PrincipalFrom: PrincipalFromContext,
	}
	ds.Register(s.grpc)

	ea := &eventarc.Service{Store: s.store, Authz: s.authz}
	ea.Mount(s.mux, principalFrom)
}
