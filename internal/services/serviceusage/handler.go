package serviceusage

import (
	"encoding/json"
	"errors"
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

// Handler serves Service Usage v1 REST methods.
type Handler struct {
	Store     *store.Store
	Authz     *authz.Evaluator
	Principal PrincipalFunc
}

// Mount registers Service Usage routes on mux.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/projects/{project}/services", h.listServices)
	mux.HandleFunc("GET /v1/projects/{project}/services/{service}", h.getService)
	mux.HandleFunc("POST /v1/projects/{project}/services/{service}", h.serviceAction)
	// batchEnable: POST /v1/projects/{project}/services:batchEnable — colon inside {servicesCol}.
	mux.HandleFunc("POST /v1/projects/{project}/{servicesCol}", h.servicesCollectionPost)
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

func (h *Handler) listServices(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project")
	resource := "projects/" + projectID
	if !h.require(w, r, "serviceusage.services.list", resource) {
		return
	}
	stateFilter, err := parseStateFilter(r.URL.Query().Get("filter"))
	if err != nil {
		gcperrors.InvalidArgument(w, err.Error())
		return
	}
	list, err := h.Store.ListServiceUsage(projectID, stateFilter)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	services := make([]map[string]any, 0, len(list))
	for _, u := range list {
		services = append(services, serviceJSON(u))
	}
	writeJSON(w, http.StatusOK, map[string]any{"services": services})
}

func (h *Handler) getService(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project")
	serviceName, action := splitColonAction(r.PathValue("service"))
	if action != "" {
		gcperrors.InvalidArgument(w, "use POST for enable/disable")
		return
	}
	resource := "projects/" + projectID
	if !h.require(w, r, "serviceusage.services.get", resource) {
		return
	}
	u, ok, err := h.Store.GetServiceUsage(projectID, serviceName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		// Never-enabled services appear as DISABLED.
		u = store.ServiceUsage{ProjectID: projectID, ServiceName: serviceName, State: "DISABLED"}
	}
	writeJSON(w, http.StatusOK, serviceJSON(u))
}

func (h *Handler) serviceAction(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project")
	raw := r.PathValue("service")
	serviceName, action := splitColonAction(raw)
	if serviceName == "" || action == "" {
		gcperrors.InvalidArgument(w, "expected services/{service}:enable|disable")
		return
	}
	resource := "projects/" + projectID
	var perm, state string
	switch action {
	case "enable":
		perm, state = "serviceusage.services.enable", "ENABLED"
	case "disable":
		perm, state = "serviceusage.services.disable", "DISABLED"
	default:
		gcperrors.InvalidArgument(w, "unknown service method")
		return
	}
	if !h.require(w, r, perm, resource) {
		return
	}
	if err := h.Store.SetServiceUsageState(projectID, serviceName, state); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	// Lab returns a completed Operation wrapping the Service (no async LRO worker).
	writeJSON(w, http.StatusOK, map[string]any{
		"name": "operations/" + action + "-" + projectID + "-" + serviceName,
		"done": true,
		"response": map[string]any{
			"@type":  "type.googleapis.com/google.api.serviceusage.v1.Service",
			"name":   "projects/" + projectID + "/services/" + serviceName,
			"parent": "projects/" + projectID,
			"state":  state,
			"config": map[string]any{"name": serviceName},
		},
	})
}

func (h *Handler) servicesCollectionPost(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project")
	col, action := splitColonAction(r.PathValue("servicesCol"))
	if col != "services" || action == "" {
		gcperrors.InvalidArgument(w, "expected services:batchEnable")
		return
	}
	if action != "batchEnable" {
		gcperrors.InvalidArgument(w, "unknown services collection method")
		return
	}
	resource := "projects/" + projectID
	if !h.require(w, r, "serviceusage.services.enable", resource) {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		gcperrors.InvalidArgument(w, "unable to read body")
		return
	}
	var req struct {
		ServiceIDs []string `json:"serviceIds"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	if err := h.Store.BatchEnableServiceUsage(projectID, req.ServiceIDs); err != nil {
		gcperrors.InvalidArgument(w, err.Error())
		return
	}
	services := make([]map[string]any, 0, len(req.ServiceIDs))
	for _, id := range req.ServiceIDs {
		id = strings.TrimSpace(id)
		services = append(services, map[string]any{
			"name":   "projects/" + projectID + "/services/" + id,
			"parent": "projects/" + projectID,
			"state":  "ENABLED",
			"config": map[string]any{"name": id},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name": "operations/batchEnable-" + projectID,
		"done": true,
		"response": map[string]any{
			"@type":    "type.googleapis.com/google.api.serviceusage.v1.BatchEnableServicesResponse",
			"services": services,
		},
	})
}

func parseStateFilter(filter string) (string, error) {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return "", nil
	}
	// Allowed: state:ENABLED | state:DISABLED (case-insensitive value).
	lower := strings.ToLower(filter)
	switch lower {
	case "state:enabled":
		return "ENABLED", nil
	case "state:disabled":
		return "DISABLED", nil
	default:
		return "", errInvalidFilter
	}
}

var errInvalidFilter = errors.New("filter must be state:ENABLED or state:DISABLED")

func serviceJSON(u store.ServiceUsage) map[string]any {
	return map[string]any{
		"name":   "projects/" + u.ProjectID + "/services/" + u.ServiceName,
		"parent": "projects/" + u.ProjectID,
		"state":  u.State,
		"config": map[string]any{
			"name": u.ServiceName,
		},
	}
}

func splitColonAction(segment string) (id, action string) {
	if i := strings.IndexByte(segment, ':'); i >= 0 {
		return segment[:i], segment[i+1:]
	}
	return segment, ""
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
