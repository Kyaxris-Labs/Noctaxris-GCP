package iam

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// MountRoles registers project custom role CRUD routes.
func (h *Handler) MountRoles(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/projects/{project}/roles", h.listRoles)
	mux.HandleFunc("POST /v1/projects/{project}/roles", h.createRole)
	mux.HandleFunc("GET /v1/projects/{project}/roles/{role}", h.getRole)
	mux.HandleFunc("PATCH /v1/projects/{project}/roles/{role}", h.patchRole)
	mux.HandleFunc("DELETE /v1/projects/{project}/roles/{role}", h.deleteRole)
}

func (h *Handler) createRole(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project")
	resource := "projects/" + projectID
	if _, ok := h.require(w, r, "iam.roles.create", resource); !ok {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		gcperrors.InvalidArgument(w, "unable to read body")
		return
	}
	var req struct {
		RoleID string `json:"roleId"`
		Role   struct {
			Title               string   `json:"title"`
			Description         string   `json:"description"`
			IncludedPermissions []string `json:"includedPermissions"`
			Stage               string   `json:"stage"`
		} `json:"role"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	roleID := strings.TrimSpace(req.RoleID)
	if roleID == "" {
		gcperrors.InvalidArgument(w, "roleId is required")
		return
	}
	created, err := h.Store.CreateCustomRole(
		projectID, roleID, req.Role.Title, req.Role.Description, req.Role.Stage, req.Role.IncludedPermissions,
	)
	if errors.Is(err, store.ErrAlreadyExists) {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "role already exists")
		return
	}
	if err != nil {
		if strings.Contains(err.Error(), "invalid roleId") {
			gcperrors.InvalidArgument(w, "roleId must be 3-64 characters matching [a-zA-Z0-9_.]")
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, roleJSON(created))
}

func (h *Handler) listRoles(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project")
	resource := "projects/" + projectID
	if _, ok := h.require(w, r, "iam.roles.list", resource); !ok {
		return
	}
	showDeleted := r.URL.Query().Get("showDeleted") == "true"
	list, err := h.Store.ListCustomRoles(projectID, showDeleted)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	roles := make([]map[string]any, 0, len(list))
	for _, role := range list {
		roles = append(roles, roleJSON(role))
	}
	writeJSON(w, http.StatusOK, map[string]any{"roles": roles})
}

func (h *Handler) getRole(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project")
	roleID := r.PathValue("role")
	resource := "projects/" + projectID
	if _, ok := h.require(w, r, "iam.roles.get", resource); !ok {
		return
	}
	name := "projects/" + projectID + "/roles/" + roleID
	role, ok, err := h.Store.GetCustomRole(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Role does not exist.")
		return
	}
	writeJSON(w, http.StatusOK, roleJSON(role))
}

func (h *Handler) patchRole(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project")
	roleID := r.PathValue("role")
	resource := "projects/" + projectID
	if _, ok := h.require(w, r, "iam.roles.update", resource); !ok {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		gcperrors.InvalidArgument(w, "unable to read body")
		return
	}
	var req struct {
		Title               string   `json:"title"`
		Description         string   `json:"description"`
		IncludedPermissions []string `json:"includedPermissions"`
		Stage               string   `json:"stage"`
		UpdateMask          string   `json:"updateMask"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	mask := req.UpdateMask
	if mask == "" {
		mask = r.URL.Query().Get("updateMask")
	}
	updateTitle := mask == "" || fieldMaskIncludes(mask, "title")
	updateDescription := mask == "" || fieldMaskIncludes(mask, "description")
	updateStage := mask == "" || fieldMaskIncludes(mask, "stage")
	updatePerms := mask == "" || fieldMaskIncludes(mask, "includedPermissions")
	if mask != "" && !updateTitle && !updateDescription && !updateStage && !updatePerms {
		gcperrors.InvalidArgument(w, "updateMask must include title, description, stage, and/or includedPermissions")
		return
	}
	name := "projects/" + projectID + "/roles/" + roleID
	updated, ok, err := h.Store.UpdateCustomRole(
		name, req.Title, req.Description, req.Stage, req.IncludedPermissions,
		updateTitle, updateDescription, updateStage, updatePerms,
	)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Role does not exist.")
		return
	}
	writeJSON(w, http.StatusOK, roleJSON(updated))
}

func (h *Handler) deleteRole(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project")
	roleID := r.PathValue("role")
	resource := "projects/" + projectID
	if _, ok := h.require(w, r, "iam.roles.delete", resource); !ok {
		return
	}
	name := "projects/" + projectID + "/roles/" + roleID
	deleted, ok, err := h.Store.DeleteCustomRole(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Role does not exist.")
		return
	}
	writeJSON(w, http.StatusOK, roleJSON(deleted))
}

func roleJSON(r store.CustomRole) map[string]any {
	perms := r.IncludedPermissions
	if perms == nil {
		perms = []string{}
	}
	return map[string]any{
		"name":                r.Name,
		"title":               r.Title,
		"description":         r.Description,
		"includedPermissions": perms,
		"stage":               r.Stage,
		"etag":                r.Etag,
		"deleted":             r.Deleted,
	}
}
