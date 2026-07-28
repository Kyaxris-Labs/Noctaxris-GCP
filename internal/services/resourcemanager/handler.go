package resourcemanager

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// PrincipalFunc extracts the authenticated principal from a request context.
type PrincipalFunc func(r *http.Request) (authn.Principal, bool)

// Handler serves Cloud Resource Manager v3 project REST methods.
type Handler struct {
	Store     *store.Store
	Authz     *authz.Evaluator
	Principal PrincipalFunc
}

// Mount registers CRM routes on mux.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v3/projects/{project}", h.handleGetProject)
	mux.HandleFunc("PATCH /v3/projects/{project}", h.handlePatchProject)
	mux.HandleFunc("POST /v3/projects/{project}", h.handleProjectPost)
}

func (h *Handler) principal(r *http.Request) (authn.Principal, bool) {
	if h.Principal != nil {
		return h.Principal(r)
	}
	return authn.Principal{}, false
}

func (h *Handler) require(w http.ResponseWriter, r *http.Request, permission, resource string) (authn.Principal, bool) {
	p, ok := h.principal(r)
	if !ok {
		gcperrors.Unauthenticated(w, "")
		return authn.Principal{}, false
	}
	allowed, err := h.Authz.Evaluate(p.Email, p.IsRoot, permission, resource)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return authn.Principal{}, false
	}
	if !allowed {
		gcperrors.PermissionDenied(w, "")
		return authn.Principal{}, false
	}
	return p, true
}

func (h *Handler) handleGetProject(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project")
	if projectID == "" || strings.Contains(projectID, ":") {
		gcperrors.InvalidArgument(w, "invalid project name")
		return
	}
	resource := "projects/" + projectID
	if _, ok := h.require(w, r, "resourcemanager.projects.get", resource); !ok {
		return
	}
	p, ok, err := h.Store.GetProject(projectID)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Requested entity was not found.")
		return
	}
	writeJSON(w, http.StatusOK, projectJSON(p))
}

func (h *Handler) handlePatchProject(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project")
	if projectID == "" || strings.Contains(projectID, ":") {
		gcperrors.InvalidArgument(w, "invalid project name")
		return
	}
	resource := "projects/" + projectID
	if _, ok := h.require(w, r, "resourcemanager.projects.update", resource); !ok {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		gcperrors.InvalidArgument(w, "unable to read body")
		return
	}
	var req struct {
		DisplayName string `json:"displayName"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	mask := r.URL.Query().Get("updateMask")
	if mask != "" && !fieldMaskIncludes(mask, "displayName") {
		gcperrors.InvalidArgument(w, "updateMask must include displayName")
		return
	}
	p, ok, err := h.Store.UpdateProjectDisplayName(projectID, req.DisplayName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Requested entity was not found.")
		return
	}
	// Lab returns the Project synchronously (GCP returns an LRO Operation).
	writeJSON(w, http.StatusOK, projectJSON(p))
}

func (h *Handler) handleProjectPost(w http.ResponseWriter, r *http.Request) {
	raw := r.PathValue("project")
	projectID, action := splitColonAction(raw)
	if projectID == "" {
		gcperrors.InvalidArgument(w, "invalid project name")
		return
	}
	resource := "projects/" + projectID
	switch action {
	case "getIamPolicy":
		h.getIamPolicy(w, r, resource)
	case "setIamPolicy":
		h.setIamPolicy(w, r, resource)
	case "testIamPermissions":
		h.testIamPermissions(w, r, resource)
	default:
		gcperrors.InvalidArgument(w, "unknown method on project")
	}
}

func (h *Handler) getIamPolicy(w http.ResponseWriter, r *http.Request, resource string) {
	if _, ok := h.require(w, r, "resourcemanager.projects.getIamPolicy", resource); !ok {
		return
	}
	raw, ok, err := h.Store.GetIAMPolicyJSON(resource)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		writeJSON(w, http.StatusOK, authz.Policy{Etag: "ACAB"})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func (h *Handler) setIamPolicy(w http.ResponseWriter, r *http.Request, resource string) {
	if _, ok := h.require(w, r, "resourcemanager.projects.setIamPolicy", resource); !ok {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		gcperrors.InvalidArgument(w, "unable to read body")
		return
	}
	var req struct {
		Policy authz.Policy `json:"policy"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	if err := h.Store.PutIAMPolicyJSON(resource, req.Policy); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, req.Policy)
}

func (h *Handler) testIamPermissions(w http.ResponseWriter, r *http.Request, resource string) {
	// GCP does not require a permission to call testIamPermissions itself.
	p, ok := h.principal(r)
	if !ok {
		gcperrors.Unauthenticated(w, "")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		gcperrors.InvalidArgument(w, "unable to read body")
		return
	}
	var req struct {
		Permissions []string `json:"permissions"`
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			gcperrors.InvalidArgument(w, "invalid JSON body")
			return
		}
	}
	granted, err := h.Authz.TestIamPermissions(p.Email, p.IsRoot, resource, req.Permissions)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if granted == nil {
		granted = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"permissions": granted})
}

func projectJSON(p store.Project) map[string]any {
	return map[string]any{
		"name":        "projects/" + p.ID,
		"projectId":   p.ID,
		"displayName": p.DisplayName,
		"state":       p.State,
		"createTime":  p.CreatedAt,
	}
}

func splitColonAction(segment string) (id, action string) {
	if i := strings.IndexByte(segment, ':'); i >= 0 {
		return segment[:i], segment[i+1:]
	}
	return segment, ""
}

func fieldMaskIncludes(mask, field string) bool {
	for _, part := range strings.Split(mask, ",") {
		if strings.TrimSpace(part) == field {
			return true
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
