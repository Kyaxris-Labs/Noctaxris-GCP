package binaryauthorization

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// Service serves Binary Authorization REST v1 (project policy lite).
type Service struct {
	Store *store.Store
	Authz *authz.Evaluator
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// Mount registers Binary Authorization v1 REST routes.
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	mux.HandleFunc("GET /v1/projects/{project}/policy", s.wrap(principalFrom, s.getPolicy))
	mux.HandleFunc("PUT /v1/projects/{project}/policy", s.wrap(principalFrom, s.updatePolicy))
}

type handlerFunc func(w http.ResponseWriter, r *http.Request, p authn.Principal)

func (s *Service) wrap(principalFrom principalFunc, h handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := principalFrom(r)
		if !ok {
			gcperrors.Unauthenticated(w, "")
			return
		}
		h(w, r, p)
	}
}

func (s *Service) require(p authn.Principal, permission, projectID string) error {
	ok, err := s.Authz.Evaluate(p.Email, p.IsRoot, permission, "projects/"+projectID)
	if err != nil {
		return err
	}
	if !ok {
		return errDenied
	}
	return nil
}

var errDenied = fmt.Errorf("permission denied")

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeAuthzErr(w http.ResponseWriter, err error) {
	if err == errDenied {
		gcperrors.PermissionDenied(w, "")
		return
	}
	gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
}

func enforcementModeFromBody(body map[string]any) string {
	if body == nil {
		return "ENFORCED_BLOCK_AND_AUDIT_LOG"
	}
	if rule, ok := body["defaultAdmissionRule"].(map[string]any); ok {
		if mode, _ := rule["enforcementMode"].(string); mode != "" {
			return mode
		}
	}
	if mode, _ := body["enforcementMode"].(string); mode != "" {
		return mode
	}
	return "ENFORCED_BLOCK_AND_AUDIT_LOG"
}

func policyJSON(project, mode, bodyJSON string) map[string]any {
	var body map[string]any
	_ = json.Unmarshal([]byte(bodyJSON), &body)
	if body == nil {
		body = map[string]any{}
	}
	out := map[string]any{
		"name": "projects/" + project + "/policy",
	}
	for k, v := range body {
		out[k] = v
	}
	if _, ok := out["defaultAdmissionRule"]; !ok {
		out["defaultAdmissionRule"] = map[string]any{
			"evaluationMode":  "REQUIRE_ATTESTATION",
			"enforcementMode": mode,
		}
	}
	return out
}

func (s *Service) getPolicy(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "binaryauthorization.policy.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	mode, bodyJSON, ok, err := s.Store.GetBinaryAuthzPolicy(project)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{
			"name": "projects/" + project + "/policy",
			"defaultAdmissionRule": map[string]any{
				"evaluationMode":  "ALWAYS_ALLOW",
				"enforcementMode": "DRYRUN_AUDIT_LOG_ONLY",
			},
		})
		return
	}
	writeJSON(w, http.StatusOK, policyJSON(project, mode, bodyJSON))
}

func (s *Service) updatePolicy(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "binaryauthorization.policy.update", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body == nil {
		body = map[string]any{}
	}
	mode := enforcementModeFromBody(body)
	raw, _ := json.Marshal(body)
	if err := s.Store.PutBinaryAuthzPolicy(project, mode, string(raw)); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, policyJSON(project, mode, string(raw)))
}
