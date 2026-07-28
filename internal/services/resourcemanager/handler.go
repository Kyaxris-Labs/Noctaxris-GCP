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
	mux.HandleFunc("GET /v3/projects", h.handleListProjects)
	mux.HandleFunc("GET /v3/projects/{project}", h.handleGetProject)
	mux.HandleFunc("PATCH /v3/projects/{project}", h.handlePatchProject)
	mux.HandleFunc("POST /v3/projects/{project}", h.handleProjectPost)
	// SearchProjects: POST /v3/projects:search — colon inside {projectsCol}.
	mux.HandleFunc("POST /v3/{projectsCol}", h.handleProjectsCollectionPost)
	// getAncestry theatre (v1): POST /v1/projects/{project}:getAncestry
	mux.HandleFunc("POST /v1/projects/{project}", h.handleProjectV1Post)

	// Organizations (seeded lab org) + folders CRUD lite.
	mux.HandleFunc("GET /v3/organizations/{organization}", h.handleGetOrganization)
	mux.HandleFunc("GET /v3/folders", h.handleListFolders)
	mux.HandleFunc("POST /v3/folders", h.handleCreateFolder)
	mux.HandleFunc("GET /v3/folders/{folder}", h.handleGetFolder)
	mux.HandleFunc("PATCH /v3/folders/{folder}", h.handlePatchFolder)
	mux.HandleFunc("DELETE /v3/folders/{folder}", h.handleDeleteFolder)
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

func (h *Handler) handleListProjects(w http.ResponseWriter, r *http.Request) {
	// Lab lists seeded projects; parent query is accepted but ignored.
	resource := "projects/-"
	if p := r.URL.Query().Get("parent"); p != "" {
		resource = p
	}
	if _, ok := h.require(w, r, "resourcemanager.projects.list", resource); !ok {
		return
	}
	list, err := h.Store.ListProjects()
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	projects := make([]map[string]any, 0, len(list))
	for _, p := range list {
		projects = append(projects, projectJSON(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": projects})
}

func (h *Handler) handleProjectsCollectionPost(w http.ResponseWriter, r *http.Request) {
	col, action := splitColonAction(r.PathValue("projectsCol"))
	if col != "projects" || action != "search" {
		gcperrors.InvalidArgument(w, "expected projects:search")
		return
	}
	if _, ok := h.require(w, r, "resourcemanager.projects.search", "projects/-"); !ok {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		gcperrors.InvalidArgument(w, "unable to read body")
		return
	}
	var req struct {
		Query string `json:"query"`
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			gcperrors.InvalidArgument(w, "invalid JSON body")
			return
		}
	}
	// Also accept query= as a URL param (gcloud convenience).
	if req.Query == "" {
		req.Query = r.URL.Query().Get("query")
	}
	list, err := h.Store.SearchProjects(req.Query)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	projects := make([]map[string]any, 0, len(list))
	for _, p := range list {
		projects = append(projects, projectJSON(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": projects})
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

func (h *Handler) handleProjectV1Post(w http.ResponseWriter, r *http.Request) {
	raw := r.PathValue("project")
	projectID, action := splitColonAction(raw)
	if projectID == "" || action == "" {
		gcperrors.InvalidArgument(w, "expected projects/{project}:getAncestry")
		return
	}
	if action != "getAncestry" {
		gcperrors.InvalidArgument(w, "unknown v1 project method")
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
	// Theatre: project under the seeded lab organization (no folder by default).
	ancestors := []map[string]any{
		{
			"resourceId": map[string]any{
				"type": "project",
				"id":   p.ID,
			},
		},
		{
			"resourceId": map[string]any{
				"type": "organization",
				"id":   store.DefaultOrganizationID,
			},
		},
	}
	writeJSON(w, http.StatusOK, map[string]any{"ancestor": ancestors})
}

func (h *Handler) handleGetOrganization(w http.ResponseWriter, r *http.Request) {
	orgID := r.PathValue("organization")
	if orgID == "" || strings.Contains(orgID, ":") {
		gcperrors.InvalidArgument(w, "invalid organization name")
		return
	}
	resource := "organizations/" + orgID
	if _, ok := h.require(w, r, "resourcemanager.organizations.get", resource); !ok {
		return
	}
	o, ok, err := h.Store.GetOrganization(orgID)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Requested entity was not found.")
		return
	}
	writeJSON(w, http.StatusOK, organizationJSON(o))
}

func (h *Handler) handleListFolders(w http.ResponseWriter, r *http.Request) {
	parent := r.URL.Query().Get("parent")
	if parent == "" {
		gcperrors.InvalidArgument(w, "parent is required")
		return
	}
	if _, ok := h.require(w, r, "resourcemanager.folders.list", parent); !ok {
		return
	}
	showDeleted := strings.EqualFold(r.URL.Query().Get("showDeleted"), "true")
	list, err := h.Store.ListFolders(parent, showDeleted)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	folders := make([]map[string]any, 0, len(list))
	for _, f := range list {
		folders = append(folders, folderJSON(f))
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": folders})
}

func (h *Handler) handleCreateFolder(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		gcperrors.InvalidArgument(w, "unable to read body")
		return
	}
	var req struct {
		Parent      string `json:"parent"`
		DisplayName string `json:"displayName"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	if req.Parent == "" {
		gcperrors.InvalidArgument(w, "parent is required")
		return
	}
	if req.DisplayName == "" {
		gcperrors.InvalidArgument(w, "displayName is required")
		return
	}
	if _, ok := h.require(w, r, "resourcemanager.folders.create", req.Parent); !ok {
		return
	}
	if strings.HasPrefix(req.Parent, "organizations/") {
		if _, ok, err := h.Store.GetOrganization(req.Parent); err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		} else if !ok {
			gcperrors.NotFound(w, "Parent organization not found.")
			return
		}
	} else if strings.HasPrefix(req.Parent, "folders/") {
		if pf, ok, err := h.Store.GetFolder(req.Parent); err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		} else if !ok || pf.State != "ACTIVE" {
			gcperrors.NotFound(w, "Parent folder not found.")
			return
		}
	} else {
		gcperrors.InvalidArgument(w, "parent must be organizations/{org} or folders/{folder}")
		return
	}
	f, created, err := h.Store.CreateFolder(store.Folder{
		Parent:      req.Parent,
		DisplayName: req.DisplayName,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "folder already exists")
		return
	}
	// Lab returns Folder synchronously (GCP returns an LRO Operation).
	writeJSON(w, http.StatusOK, folderJSON(f))
}

func (h *Handler) handleGetFolder(w http.ResponseWriter, r *http.Request) {
	folderID := r.PathValue("folder")
	if folderID == "" || strings.Contains(folderID, ":") {
		gcperrors.InvalidArgument(w, "invalid folder name")
		return
	}
	resource := "folders/" + folderID
	if _, ok := h.require(w, r, "resourcemanager.folders.get", resource); !ok {
		return
	}
	f, ok, err := h.Store.GetFolder(folderID)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Requested entity was not found.")
		return
	}
	writeJSON(w, http.StatusOK, folderJSON(f))
}

func (h *Handler) handlePatchFolder(w http.ResponseWriter, r *http.Request) {
	folderID := r.PathValue("folder")
	if folderID == "" || strings.Contains(folderID, ":") {
		gcperrors.InvalidArgument(w, "invalid folder name")
		return
	}
	resource := "folders/" + folderID
	if _, ok := h.require(w, r, "resourcemanager.folders.update", resource); !ok {
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
	if mask != "" && !fieldMaskIncludes(mask, "displayName") && !fieldMaskIncludes(mask, "display_name") {
		gcperrors.InvalidArgument(w, "updateMask must include displayName")
		return
	}
	f, ok, err := h.Store.UpdateFolderDisplayName(folderID, req.DisplayName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Requested entity was not found.")
		return
	}
	writeJSON(w, http.StatusOK, folderJSON(f))
}

func (h *Handler) handleDeleteFolder(w http.ResponseWriter, r *http.Request) {
	folderID := r.PathValue("folder")
	if folderID == "" || strings.Contains(folderID, ":") {
		gcperrors.InvalidArgument(w, "invalid folder name")
		return
	}
	resource := "folders/" + folderID
	if _, ok := h.require(w, r, "resourcemanager.folders.delete", resource); !ok {
		return
	}
	f, ok, err := h.Store.DeleteFolder(folderID)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Requested entity was not found.")
		return
	}
	writeJSON(w, http.StatusOK, folderJSON(f))
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
		// Lab theatre: seeded projects hang under the default organization.
		"parent": store.DefaultOrganizationName,
	}
}

func organizationJSON(o store.Organization) map[string]any {
	return map[string]any{
		"name":        o.Name,
		"displayName": o.DisplayName,
		"state":       o.State,
		"createTime":  o.CreatedAt,
		"updateTime":  o.UpdatedAt,
	}
}

func folderJSON(f store.Folder) map[string]any {
	out := map[string]any{
		"name":        f.Name,
		"parent":      f.Parent,
		"displayName": f.DisplayName,
		"state":       f.State,
		"createTime":  f.CreatedAt,
		"updateTime":  f.UpdatedAt,
		"etag":        f.Etag,
	}
	if f.DeleteTime != "" {
		out["deleteTime"] = f.DeleteTime
	}
	return out
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
