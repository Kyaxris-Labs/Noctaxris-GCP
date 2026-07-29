package server

import (
	"context"
	"net/http"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/audit"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/logging"
)

// registerCloudAudit mounts env-gated Cloud Audit Logs inject and mirrors live
// audit.Writer events into the CAL SQLite table when a Writer is present.
//
// Wire from Server.New as: s.registerCloudAudit()
// Also wire store: if err := s.migrateCloudAudit(); err != nil { return err }
func (s *Server) registerCloudAudit() {
	principalFrom := func(r *http.Request) (authn.Principal, bool) {
		return PrincipalFromContext(r.Context())
	}
	logSvc := &logging.Service{Store: s.store, Authz: s.authz}
	logSvc.MountLab(s.mux, principalFrom, s.cfg.ProjectID)

	if s.audit != nil {
		projectID := s.cfg.ProjectID
		st := s.store
		s.audit.SetSink(func(ctx context.Context, ev audit.Event) error {
			return st.WriteCloudAuditFromKernelEvent(projectID, ev)
		})
	}
}
