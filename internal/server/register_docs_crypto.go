package server

import (
	"net/http"

	"cloud.google.com/go/firestore/apiv1/firestorepb"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/firestore"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/kms"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/logging"
)

// registerDocsCrypto registers Firestore (gRPC), Cloud KMS (REST), and Cloud Logging (REST).
// Requires s.grpc to already exist (created by registerIdentity / newGRPCServer).
func (s *Server) registerDocsCrypto() {
	if s.grpc == nil {
		s.grpc = s.newGRPCServer()
	}

	fs := &firestore.Service{
		Store:         s.store,
		Authn:         s.authn,
		Authz:         s.authz,
		PrincipalFrom: PrincipalFromContext,
	}
	firestorepb.RegisterFirestoreServer(s.grpc, fs)

	principalFrom := func(r *http.Request) (authn.Principal, bool) {
		return PrincipalFromContext(r.Context())
	}

	kmsSvc := &kms.Service{Store: s.store, Authz: s.authz}
	kmsSvc.Mount(s.mux, principalFrom)

	logSvc := &logging.Service{Store: s.store, Authz: s.authz}
	logSvc.Mount(s.mux, principalFrom)
}
