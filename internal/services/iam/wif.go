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

// MountWIF registers Workload Identity Federation pool/provider CRUD.
// Pair with MountSTS (POST /v1/token) for lab token-exchange into wif:{provider}:{subject}.
func (h *Handler) MountWIF(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/workloadIdentityPools", h.listWIFPools)
	mux.HandleFunc("POST /v1/projects/{project}/locations/{location}/workloadIdentityPools", h.createWIFPool)
	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/workloadIdentityPools/{pool}", h.getWIFPool)
	mux.HandleFunc("DELETE /v1/projects/{project}/locations/{location}/workloadIdentityPools/{pool}", h.deleteWIFPool)

	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/workloadIdentityPools/{pool}/providers", h.listWIFProviders)
	mux.HandleFunc("POST /v1/projects/{project}/locations/{location}/workloadIdentityPools/{pool}/providers", h.createWIFProvider)
	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/workloadIdentityPools/{pool}/providers/{provider}", h.getWIFProvider)
	mux.HandleFunc("DELETE /v1/projects/{project}/locations/{location}/workloadIdentityPools/{pool}/providers/{provider}", h.deleteWIFProvider)
}

func (h *Handler) createWIFPool(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project")
	location := r.PathValue("location")
	resource := "projects/" + projectID
	if _, ok := h.require(w, r, "iam.workloadIdentityPools.create", resource); !ok {
		return
	}
	poolID := strings.TrimSpace(r.URL.Query().Get("workloadIdentityPoolId"))
	if poolID == "" {
		gcperrors.InvalidArgument(w, "workloadIdentityPoolId query parameter is required")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		gcperrors.InvalidArgument(w, "unable to read body")
		return
	}
	var req struct {
		DisplayName string `json:"displayName"`
		Description string `json:"description"`
		Disabled    bool   `json:"disabled"`
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			gcperrors.InvalidArgument(w, "invalid JSON body")
			return
		}
	}
	p, err := h.Store.CreateWIFPool(projectID, location, poolID, req.DisplayName, req.Description, req.Disabled)
	if errors.Is(err, store.ErrAlreadyExists) {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "workload identity pool already exists")
		return
	}
	if err != nil {
		if strings.Contains(err.Error(), "invalid") {
			gcperrors.InvalidArgument(w, err.Error())
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wifPoolJSON(p))
}

func (h *Handler) listWIFPools(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project")
	location := r.PathValue("location")
	if _, ok := h.require(w, r, "iam.workloadIdentityPools.list", "projects/"+projectID); !ok {
		return
	}
	showDeleted := r.URL.Query().Get("showDeleted") == "true"
	list, err := h.Store.ListWIFPools(projectID, location, showDeleted)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, p := range list {
		out = append(out, wifPoolJSON(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"workloadIdentityPools": out})
}

func (h *Handler) getWIFPool(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project")
	location := r.PathValue("location")
	poolID, _ := splitColonAction(r.PathValue("pool"))
	if _, ok := h.require(w, r, "iam.workloadIdentityPools.get", "projects/"+projectID); !ok {
		return
	}
	name := "projects/" + projectID + "/locations/" + location + "/workloadIdentityPools/" + poolID
	p, ok, err := h.Store.GetWIFPool(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Workload identity pool does not exist.")
		return
	}
	writeJSON(w, http.StatusOK, wifPoolJSON(p))
}

func (h *Handler) deleteWIFPool(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project")
	location := r.PathValue("location")
	poolID, _ := splitColonAction(r.PathValue("pool"))
	if _, ok := h.require(w, r, "iam.workloadIdentityPools.delete", "projects/"+projectID); !ok {
		return
	}
	name := "projects/" + projectID + "/locations/" + location + "/workloadIdentityPools/" + poolID
	p, ok, err := h.Store.DeleteWIFPool(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Workload identity pool does not exist.")
		return
	}
	writeJSON(w, http.StatusOK, wifPoolJSON(p))
}

func (h *Handler) createWIFProvider(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project")
	location := r.PathValue("location")
	poolID, _ := splitColonAction(r.PathValue("pool"))
	if _, ok := h.require(w, r, "iam.workloadIdentityPoolProviders.create", "projects/"+projectID); !ok {
		return
	}
	providerID := strings.TrimSpace(r.URL.Query().Get("workloadIdentityPoolProviderId"))
	if providerID == "" {
		gcperrors.InvalidArgument(w, "workloadIdentityPoolProviderId query parameter is required")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		gcperrors.InvalidArgument(w, "unable to read body")
		return
	}
	var req struct {
		DisplayName      string            `json:"displayName"`
		Description      string            `json:"description"`
		Disabled         bool              `json:"disabled"`
		AttributeMapping map[string]string `json:"attributeMapping"`
		Oidc             *struct {
			IssuerUri string `json:"issuerUri"`
		} `json:"oidc"`
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			gcperrors.InvalidArgument(w, "invalid JSON body")
			return
		}
	}
	attrJSON := "{}"
	if len(req.AttributeMapping) > 0 {
		raw, err := json.Marshal(req.AttributeMapping)
		if err != nil {
			gcperrors.InvalidArgument(w, "invalid attributeMapping")
			return
		}
		attrJSON = string(raw)
	}
	issuer := ""
	if req.Oidc != nil {
		issuer = req.Oidc.IssuerUri
	}
	poolName := "projects/" + projectID + "/locations/" + location + "/workloadIdentityPools/" + poolID
	p, err := h.Store.CreateWIFProvider(poolName, providerID, req.DisplayName, req.Description, issuer, attrJSON, req.Disabled)
	if errors.Is(err, store.ErrAlreadyExists) {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "workload identity pool provider already exists")
		return
	}
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			gcperrors.NotFound(w, "Workload identity pool does not exist.")
			return
		}
		if strings.Contains(err.Error(), "invalid") {
			gcperrors.InvalidArgument(w, err.Error())
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wifProviderJSON(p))
}

func (h *Handler) listWIFProviders(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project")
	location := r.PathValue("location")
	poolID, _ := splitColonAction(r.PathValue("pool"))
	if _, ok := h.require(w, r, "iam.workloadIdentityPoolProviders.list", "projects/"+projectID); !ok {
		return
	}
	poolName := "projects/" + projectID + "/locations/" + location + "/workloadIdentityPools/" + poolID
	showDeleted := r.URL.Query().Get("showDeleted") == "true"
	list, err := h.Store.ListWIFProviders(poolName, showDeleted)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, p := range list {
		out = append(out, wifProviderJSON(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"workloadIdentityPoolProviders": out})
}

func (h *Handler) getWIFProvider(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project")
	location := r.PathValue("location")
	poolID, _ := splitColonAction(r.PathValue("pool"))
	providerID, _ := splitColonAction(r.PathValue("provider"))
	if _, ok := h.require(w, r, "iam.workloadIdentityPoolProviders.get", "projects/"+projectID); !ok {
		return
	}
	name := "projects/" + projectID + "/locations/" + location + "/workloadIdentityPools/" + poolID + "/providers/" + providerID
	p, ok, err := h.Store.GetWIFProvider(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Workload identity pool provider does not exist.")
		return
	}
	writeJSON(w, http.StatusOK, wifProviderJSON(p))
}

func (h *Handler) deleteWIFProvider(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project")
	location := r.PathValue("location")
	poolID, _ := splitColonAction(r.PathValue("pool"))
	providerID, _ := splitColonAction(r.PathValue("provider"))
	if _, ok := h.require(w, r, "iam.workloadIdentityPoolProviders.delete", "projects/"+projectID); !ok {
		return
	}
	name := "projects/" + projectID + "/locations/" + location + "/workloadIdentityPools/" + poolID + "/providers/" + providerID
	p, ok, err := h.Store.DeleteWIFProvider(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Workload identity pool provider does not exist.")
		return
	}
	writeJSON(w, http.StatusOK, wifProviderJSON(p))
}

func wifPoolJSON(p store.WorkloadIdentityPool) map[string]any {
	return map[string]any{
		"name":        p.Name,
		"displayName": p.DisplayName,
		"description": p.Description,
		"state":       p.State,
		"disabled":    p.Disabled,
	}
}

func wifProviderJSON(p store.WorkloadIdentityPoolProvider) map[string]any {
	out := map[string]any{
		"name":        p.Name,
		"displayName": p.DisplayName,
		"description": p.Description,
		"state":       p.State,
		"disabled":    p.Disabled,
	}
	var attrs map[string]string
	if p.AttributeMap != "" && p.AttributeMap != "{}" {
		_ = json.Unmarshal([]byte(p.AttributeMap), &attrs)
	}
	if attrs == nil {
		attrs = map[string]string{}
	}
	out["attributeMapping"] = attrs
	if p.IssuerURI != "" {
		out["oidc"] = map[string]any{"issuerUri": p.IssuerURI}
	}
	return out
}
