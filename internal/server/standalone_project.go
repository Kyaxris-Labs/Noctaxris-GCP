package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

const standaloneProjectSeedPath = "/_noctaxris-gcp/lab/projects:seedStandalone"

func (s *Server) registerStandaloneProject() {
	if !s.cfg.LabStandalone {
		return
	}
	s.mux.HandleFunc("POST "+standaloneProjectSeedPath, s.handleSeedStandaloneProject)
}

func (s *Server) handleSeedStandaloneProject(w http.ResponseWriter, r *http.Request) {
	p, ok := PrincipalFromContext(r.Context())
	if !ok {
		gcperrors.Unauthenticated(w, "")
		return
	}
	if !p.IsRoot {
		gcperrors.PermissionDenied(w, "Standalone project seed requires the root bearer token.")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		gcperrors.InvalidArgument(w, "unable to read body")
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			gcperrors.InvalidArgument(w, "invalid JSON body")
			return
		}
	}
	if strings.TrimSpace(req.Name) == "" {
		gcperrors.InvalidArgument(w, "name is required")
		return
	}
	out, err := s.store.SeedStandaloneProject(req.Name, s.cfg.RootServiceAccount, s.cfg.RootAccessToken)
	if err != nil {
		if errors.Is(err, store.ErrInvalidSeedName) {
			gcperrors.InvalidArgument(w, "name is not a valid project id")
			return
		}
		if errors.Is(err, store.ErrAlreadyExists) {
			gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "project already exists")
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "standalone seed failed")
		return
	}
	if !s.standaloneSeedReturnable(out) {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "standalone seed failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name":                "projects/" + out.ProjectID,
		"projectId":           out.ProjectID,
		"serviceAccountEmail": out.ServiceAccountEmail,
		"accessToken":         out.AccessToken,
	})
}

// standaloneSeedReturnable rejects the root token and a principal that authenticates as root.
// A lookup failure still allows the stored token through so a repeat call can return it.
func (s *Server) standaloneSeedReturnable(out store.StandaloneProject) bool {
	if out.AccessToken == "" || out.ServiceAccountEmail == "" || out.ProjectID == "" {
		return false
	}
	if s.cfg.RootAccessToken != "" && out.AccessToken == s.cfg.RootAccessToken {
		return false
	}
	if s.cfg.RootServiceAccount != "" && strings.EqualFold(out.ServiceAccountEmail, s.cfg.RootServiceAccount) {
		return false
	}
	principal, err := s.authn.AuthenticateToken(out.AccessToken)
	if err != nil {
		return true
	}
	if principal.IsRoot || !strings.EqualFold(principal.Email, out.ServiceAccountEmail) {
		return false
	}
	return true
}
