package orgpolicy

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// PrincipalFunc extracts the authenticated principal from a request context.
type PrincipalFunc func(r *http.Request) (authn.Principal, bool)

// Handler serves Organization Policy API v2 REST theatre
// (projects/folders/organizations policies + known constraints).
type Handler struct {
	Store     *store.Store
	Authz     *authz.Evaluator
	Principal PrincipalFunc
}

// Mount registers Org Policy v2 routes on mux.
func (h *Handler) Mount(mux *http.ServeMux) {
	// Projects
	mux.HandleFunc("GET /v2/projects/{project}/policies", h.listPoliciesProject)
	mux.HandleFunc("POST /v2/projects/{project}/policies", h.createPolicyProject)
	mux.HandleFunc("GET /v2/projects/{project}/policies/{constraint}", h.getPolicyProject)
	mux.HandleFunc("PATCH /v2/projects/{project}/policies/{constraint}", h.patchPolicyProject)
	mux.HandleFunc("DELETE /v2/projects/{project}/policies/{constraint}", h.deletePolicyProject)
	mux.HandleFunc("GET /v2/projects/{project}/constraints", h.listConstraintsProject)

	// Folders
	mux.HandleFunc("GET /v2/folders/{folder}/policies", h.listPoliciesFolder)
	mux.HandleFunc("POST /v2/folders/{folder}/policies", h.createPolicyFolder)
	mux.HandleFunc("GET /v2/folders/{folder}/policies/{constraint}", h.getPolicyFolder)
	mux.HandleFunc("PATCH /v2/folders/{folder}/policies/{constraint}", h.patchPolicyFolder)
	mux.HandleFunc("DELETE /v2/folders/{folder}/policies/{constraint}", h.deletePolicyFolder)
	mux.HandleFunc("GET /v2/folders/{folder}/constraints", h.listConstraintsFolder)

	// Organizations
	mux.HandleFunc("GET /v2/organizations/{org}/policies", h.listPoliciesOrg)
	mux.HandleFunc("POST /v2/organizations/{org}/policies", h.createPolicyOrg)
	mux.HandleFunc("GET /v2/organizations/{org}/policies/{constraint}", h.getPolicyOrg)
	mux.HandleFunc("PATCH /v2/organizations/{org}/policies/{constraint}", h.patchPolicyOrg)
	mux.HandleFunc("DELETE /v2/organizations/{org}/policies/{constraint}", h.deletePolicyOrg)
	mux.HandleFunc("GET /v2/organizations/{org}/constraints", h.listConstraintsOrg)
}

func (h *Handler) principal(r *http.Request) (authn.Principal, bool) {
	if h.Principal != nil {
		return h.Principal(r)
	}
	return authn.Principal{}, false
}

func (h *Handler) require(w http.ResponseWriter, r *http.Request, permission, resource string) bool {
	p, ok := h.principal(r)
	if !ok {
		gcperrors.Unauthenticated(w, "")
		return false
	}
	allowed, err := h.Authz.Evaluate(p.Email, p.IsRoot, permission, resource)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return false
	}
	if !allowed {
		gcperrors.PermissionDenied(w, "")
		return false
	}
	return true
}

func (h *Handler) listPoliciesProject(w http.ResponseWriter, r *http.Request) {
	h.listPolicies(w, r, "projects/"+r.PathValue("project"))
}
func (h *Handler) createPolicyProject(w http.ResponseWriter, r *http.Request) {
	h.createPolicy(w, r, "projects/"+r.PathValue("project"))
}
func (h *Handler) getPolicyProject(w http.ResponseWriter, r *http.Request) {
	h.getPolicy(w, r, "projects/"+r.PathValue("project"), r.PathValue("constraint"))
}
func (h *Handler) patchPolicyProject(w http.ResponseWriter, r *http.Request) {
	h.patchPolicy(w, r, "projects/"+r.PathValue("project"), r.PathValue("constraint"))
}
func (h *Handler) deletePolicyProject(w http.ResponseWriter, r *http.Request) {
	h.deletePolicy(w, r, "projects/"+r.PathValue("project"), r.PathValue("constraint"))
}
func (h *Handler) listConstraintsProject(w http.ResponseWriter, r *http.Request) {
	h.listConstraints(w, r, "projects/"+r.PathValue("project"))
}

func (h *Handler) listPoliciesFolder(w http.ResponseWriter, r *http.Request) {
	h.listPolicies(w, r, "folders/"+r.PathValue("folder"))
}
func (h *Handler) createPolicyFolder(w http.ResponseWriter, r *http.Request) {
	h.createPolicy(w, r, "folders/"+r.PathValue("folder"))
}
func (h *Handler) getPolicyFolder(w http.ResponseWriter, r *http.Request) {
	h.getPolicy(w, r, "folders/"+r.PathValue("folder"), r.PathValue("constraint"))
}
func (h *Handler) patchPolicyFolder(w http.ResponseWriter, r *http.Request) {
	h.patchPolicy(w, r, "folders/"+r.PathValue("folder"), r.PathValue("constraint"))
}
func (h *Handler) deletePolicyFolder(w http.ResponseWriter, r *http.Request) {
	h.deletePolicy(w, r, "folders/"+r.PathValue("folder"), r.PathValue("constraint"))
}
func (h *Handler) listConstraintsFolder(w http.ResponseWriter, r *http.Request) {
	h.listConstraints(w, r, "folders/"+r.PathValue("folder"))
}

func (h *Handler) listPoliciesOrg(w http.ResponseWriter, r *http.Request) {
	h.listPolicies(w, r, "organizations/"+r.PathValue("org"))
}
func (h *Handler) createPolicyOrg(w http.ResponseWriter, r *http.Request) {
	h.createPolicy(w, r, "organizations/"+r.PathValue("org"))
}
func (h *Handler) getPolicyOrg(w http.ResponseWriter, r *http.Request) {
	h.getPolicy(w, r, "organizations/"+r.PathValue("org"), r.PathValue("constraint"))
}
func (h *Handler) patchPolicyOrg(w http.ResponseWriter, r *http.Request) {
	h.patchPolicy(w, r, "organizations/"+r.PathValue("org"), r.PathValue("constraint"))
}
func (h *Handler) deletePolicyOrg(w http.ResponseWriter, r *http.Request) {
	h.deletePolicy(w, r, "organizations/"+r.PathValue("org"), r.PathValue("constraint"))
}
func (h *Handler) listConstraintsOrg(w http.ResponseWriter, r *http.Request) {
	h.listConstraints(w, r, "organizations/"+r.PathValue("org"))
}

func (h *Handler) listPolicies(w http.ResponseWriter, r *http.Request, parent string) {
	if !h.require(w, r, "orgpolicy.policies.list", parent) {
		return
	}
	list, err := h.Store.ListOrgPolicies(parent)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	policies := make([]map[string]any, 0, len(list))
	for _, p := range list {
		policies = append(policies, policyJSON(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"policies": policies})
}

func (h *Handler) getPolicy(w http.ResponseWriter, r *http.Request, parent, constraint string) {
	if !h.require(w, r, "orgpolicy.policies.get", parent) {
		return
	}
	constraint, action := splitColonAction(constraint)
	if action == "getEffectivePolicy" {
		h.getEffectivePolicy(w, r, parent, constraint)
		return
	}
	if action != "" {
		gcperrors.InvalidArgument(w, "unknown policy method")
		return
	}
	p, ok, err := h.Store.GetOrgPolicy(parent, constraint)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Requested entity was not found.")
		return
	}
	writeJSON(w, http.StatusOK, policyJSON(p))
}

func (h *Handler) getEffectivePolicy(w http.ResponseWriter, r *http.Request, parent, constraint string) {
	enforced, err := h.Store.IsOrgPolicyConstraintEnforced(parent, constraint)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	name := parent + "/policies/" + store.NormalizeOrgConstraint(constraint)
	writeJSON(w, http.StatusOK, map[string]any{
		"name": name,
		"spec": map[string]any{
			"rules":      []map[string]any{{"enforce": enforced}},
			"updateTime": time.Now().UTC().Format(time.RFC3339Nano),
		},
	})
}

func (h *Handler) createPolicy(w http.ResponseWriter, r *http.Request, parent string) {
	if !h.require(w, r, "orgpolicy.policies.create", parent) {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		gcperrors.InvalidArgument(w, "unable to read body")
		return
	}
	var req policyBody
	if err := json.Unmarshal(body, &req); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	constraint := store.NormalizeOrgConstraint(r.URL.Query().Get("constraint"))
	if constraint == "" {
		constraint = constraintFromName(req.Name)
	}
	if constraint == "" {
		gcperrors.InvalidArgument(w, "constraint query or policy.name required")
		return
	}
	if !store.IsKnownOrgConstraint(constraint) {
		gcperrors.InvalidArgument(w, "unknown constraint")
		return
	}
	if _, ok, err := h.Store.GetOrgPolicy(parent, constraint); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if ok {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "policy already exists")
		return
	}
	specJSON, err := marshalSpec(req.Spec)
	if err != nil {
		gcperrors.InvalidArgument(w, err.Error())
		return
	}
	p, err := h.Store.SetOrgPolicy(parent, constraint, specJSON)
	if err != nil {
		gcperrors.InvalidArgument(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, policyJSON(p))
}

func (h *Handler) patchPolicy(w http.ResponseWriter, r *http.Request, parent, constraint string) {
	if !h.require(w, r, "orgpolicy.policies.update", parent) {
		return
	}
	constraint = store.NormalizeOrgConstraint(constraint)
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		gcperrors.InvalidArgument(w, "unable to read body")
		return
	}
	var req policyBody
	if err := json.Unmarshal(body, &req); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	if !store.IsKnownOrgConstraint(constraint) {
		gcperrors.InvalidArgument(w, "unknown constraint")
		return
	}
	specJSON, err := marshalSpec(req.Spec)
	if err != nil {
		gcperrors.InvalidArgument(w, err.Error())
		return
	}
	p, err := h.Store.SetOrgPolicy(parent, constraint, specJSON)
	if err != nil {
		gcperrors.InvalidArgument(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, policyJSON(p))
}

func (h *Handler) deletePolicy(w http.ResponseWriter, r *http.Request, parent, constraint string) {
	if !h.require(w, r, "orgpolicy.policies.delete", parent) {
		return
	}
	ok, err := h.Store.DeleteOrgPolicy(parent, constraint)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Requested entity was not found.")
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("{}"))
}

func (h *Handler) listConstraints(w http.ResponseWriter, r *http.Request, parent string) {
	if !h.require(w, r, "orgpolicy.constraints.list", parent) {
		return
	}
	constraints := make([]map[string]any, 0, len(store.KnownOrgPolicyConstraints()))
	for _, id := range store.KnownOrgPolicyConstraints() {
		constraints = append(constraints, map[string]any{
			"name":         parent + "/constraints/" + id,
			"constraintDefault": "ALLOW",
			"displayName":  id,
			"description":  labConstraintDescription(id),
			"booleanConstraint": map[string]any{},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"constraints": constraints})
}

type policyBody struct {
	Name string         `json:"name"`
	Spec map[string]any `json:"spec"`
	Etag string         `json:"etag"`
}

func marshalSpec(spec map[string]any) (string, error) {
	if spec == nil {
		return `{"rules":[{"enforce":false}]}`, nil
	}
	b, err := json.Marshal(spec)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func policyJSON(p store.OrgPolicy) map[string]any {
	var spec any = map[string]any{}
	_ = json.Unmarshal([]byte(p.SpecJSON), &spec)
	if m, ok := spec.(map[string]any); ok {
		if _, has := m["updateTime"]; !has && p.UpdatedAt != "" {
			m["updateTime"] = p.UpdatedAt
			spec = m
		}
	}
	return map[string]any{
		"name": p.Name,
		"spec": spec,
		"etag": p.Etag,
	}
}

func constraintFromName(name string) string {
	name = strings.TrimSpace(name)
	const marker = "/policies/"
	i := strings.LastIndex(name, marker)
	if i < 0 {
		return store.NormalizeOrgConstraint(name)
	}
	return store.NormalizeOrgConstraint(name[i+len(marker):])
}

func splitColonAction(seg string) (name, action string) {
	if i := strings.Index(seg, ":"); i >= 0 {
		return seg[:i], seg[i+1:]
	}
	return seg, ""
}

func labConstraintDescription(id string) string {
	switch id {
	case store.ConstraintDisableServiceAccountKeyCreation:
		return "When enforced, disables creating user-managed service account keys (lab theatre)."
	case store.ConstraintStoragePublicAccessPrevention:
		return "When enforced, rejects bucket IAM bindings with allUsers or allAuthenticatedUsers (lab theatre)."
	default:
		return ""
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
