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
	// batchEnable / batchGet: colon inside {servicesCol}.
	mux.HandleFunc("POST /v1/projects/{project}/{servicesCol}", h.servicesCollectionPost)
	mux.HandleFunc("GET /v1/projects/{project}/{servicesCol}", h.servicesCollectionGet)
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
			"config": serviceConfig(serviceName),
		},
	})
}

func (h *Handler) servicesCollectionGet(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project")
	col, action := splitColonAction(r.PathValue("servicesCol"))
	if col != "services" || action != "batchGet" {
		gcperrors.InvalidArgument(w, "expected services:batchGet")
		return
	}
	h.batchGet(w, r, projectID)
}

func (h *Handler) servicesCollectionPost(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project")
	col, action := splitColonAction(r.PathValue("servicesCol"))
	if col != "services" || action == "" {
		gcperrors.InvalidArgument(w, "expected services:batchEnable|batchDisable|batchGet")
		return
	}
	switch action {
	case "batchEnable":
		h.batchEnable(w, r, projectID)
	case "batchDisable":
		h.batchDisable(w, r, projectID)
	case "batchGet":
		// Accept POST body {"names":[...]} in addition to official GET + query.
		h.batchGet(w, r, projectID)
	default:
		gcperrors.InvalidArgument(w, "unknown services collection method")
	}
}

func (h *Handler) batchEnable(w http.ResponseWriter, r *http.Request, projectID string) {
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
			"config": serviceConfig(id),
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

func (h *Handler) batchDisable(w http.ResponseWriter, r *http.Request, projectID string) {
	resource := "projects/" + projectID
	if !h.require(w, r, "serviceusage.services.disable", resource) {
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
	if err := h.Store.BatchDisableServiceUsage(projectID, req.ServiceIDs); err != nil {
		gcperrors.InvalidArgument(w, err.Error())
		return
	}
	services := make([]map[string]any, 0, len(req.ServiceIDs))
	for _, id := range req.ServiceIDs {
		id = strings.TrimSpace(id)
		services = append(services, map[string]any{
			"name":   "projects/" + projectID + "/services/" + id,
			"parent": "projects/" + projectID,
			"state":  "DISABLED",
			"config": serviceConfig(id),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name": "operations/batchDisable-" + projectID,
		"done": true,
		"response": map[string]any{
			"@type":    "type.googleapis.com/google.api.serviceusage.v1.BatchDisableServicesResponse",
			"services": services,
		},
	})
}

func (h *Handler) batchGet(w http.ResponseWriter, r *http.Request, projectID string) {
	resource := "projects/" + projectID
	if !h.require(w, r, "serviceusage.services.get", resource) {
		return
	}
	names := r.URL.Query()["names"]
	if len(names) == 0 {
		// Also accept repeated names= and comma-separated.
		if raw := r.URL.Query().Get("names"); raw != "" {
			names = strings.Split(raw, ",")
		}
	}
	if len(names) == 0 && r.Body != nil {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err == nil && len(body) > 0 {
			var req struct {
				Names []string `json:"names"`
			}
			if err := json.Unmarshal(body, &req); err != nil {
				gcperrors.InvalidArgument(w, "invalid JSON body")
				return
			}
			names = req.Names
		}
	}
	serviceNames := make([]string, 0, len(names))
	prefix := "projects/" + projectID + "/services/"
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if strings.HasPrefix(n, prefix) {
			n = strings.TrimPrefix(n, prefix)
		} else if strings.Contains(n, "/services/") {
			parts := strings.SplitN(n, "/services/", 2)
			if len(parts) == 2 {
				n = parts[1]
			}
		}
		serviceNames = append(serviceNames, n)
	}
	list, err := h.Store.BatchGetServiceUsage(projectID, serviceNames)
	if err != nil {
		gcperrors.InvalidArgument(w, err.Error())
		return
	}
	services := make([]map[string]any, 0, len(list))
	for _, u := range list {
		services = append(services, serviceJSON(u))
	}
	writeJSON(w, http.StatusOK, map[string]any{"services": services})
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
		"config": serviceConfig(u.ServiceName),
	}
}

func serviceConfig(serviceName string) map[string]any {
	cfg := map[string]any{
		"name": serviceName,
		"apis": []map[string]any{{"name": serviceName}},
	}
	if title := serviceTitle(serviceName); title != "" {
		cfg["title"] = title
		cfg["documentation"] = map[string]any{
			"summary": title,
		}
	}
	return cfg
}

func serviceTitle(serviceName string) string {
	titles := map[string]string{
		"cloudresourcemanager.googleapis.com": "Cloud Resource Manager API",
		"iam.googleapis.com":                  "Identity and Access Management (IAM) API",
		"serviceusage.googleapis.com":         "Service Usage API",
		"storage.googleapis.com":              "Cloud Storage API",
		"pubsub.googleapis.com":               "Cloud Pub/Sub API",
		"secretmanager.googleapis.com":        "Secret Manager API",
		"firestore.googleapis.com":            "Cloud Firestore API",
		"cloudkms.googleapis.com":             "Cloud Key Management Service (KMS) API",
		"logging.googleapis.com":              "Cloud Logging API",
		"run.googleapis.com":                  "Cloud Run Admin API",
		"cloudfunctions.googleapis.com":       "Cloud Functions API",
		"cloudscheduler.googleapis.com":       "Cloud Scheduler API",
		"cloudtasks.googleapis.com":           "Cloud Tasks API",
		"bigquery.googleapis.com":             "BigQuery API",
		"identitytoolkit.googleapis.com":      "Identity Toolkit API",
		"monitoring.googleapis.com":           "Cloud Monitoring API",
		"datastore.googleapis.com":            "Cloud Datastore API",
		"eventarc.googleapis.com":             "Eventarc API",
		"appengine.googleapis.com":            "App Engine Admin API",
		"artifactregistry.googleapis.com":     "Artifact Registry API",
		"cloudbuild.googleapis.com":           "Cloud Build API",
		"workflows.googleapis.com":            "Workflows API",
		"spanner.googleapis.com":              "Cloud Spanner API",
		"compute.googleapis.com":              "Compute Engine API",
		"dns.googleapis.com":                  "Cloud DNS API",
		"dataflow.googleapis.com":             "Dataflow API",
		"bigtableadmin.googleapis.com":        "Cloud Bigtable Admin API",
		"redis.googleapis.com":                "Google Cloud Memorystore for Redis API",
		"certificatemanager.googleapis.com":   "Certificate Manager API",
		"file.googleapis.com":                 "Cloud Filestore API",
		"aiplatform.googleapis.com":           "Vertex AI API",
	}
	return titles[serviceName]
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
