package resourcemanager

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// MountTags registers TagKeys / TagBindings lite routes.
func (h *Handler) MountTags(mux *http.ServeMux) {
	mux.HandleFunc("GET /v3/tagKeys", h.handleListTagKeys)
	mux.HandleFunc("POST /v3/tagKeys", h.handleCreateTagKey)
	mux.HandleFunc("GET /v3/tagKeys/{tagKey}", h.handleGetTagKey)
	mux.HandleFunc("DELETE /v3/tagKeys/{tagKey}", h.handleDeleteTagKey)

	mux.HandleFunc("GET /v3/tagBindings", h.handleListTagBindings)
	mux.HandleFunc("POST /v3/tagBindings", h.handleCreateTagBinding)
	mux.HandleFunc("GET /v3/tagBindings/{tagBinding}", h.handleGetTagBinding)
	mux.HandleFunc("DELETE /v3/tagBindings/{tagBinding}", h.handleDeleteTagBinding)
}

func (h *Handler) handleCreateTagKey(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		gcperrors.InvalidArgument(w, "unable to read body")
		return
	}
	var req struct {
		Parent      string `json:"parent"`
		ShortName   string `json:"shortName"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	if _, ok := h.require(w, r, "resourcemanager.tagKeys.create", req.Parent); !ok {
		return
	}
	k, err := h.Store.CreateTagKey(req.Parent, req.ShortName, req.Description)
	if errors.Is(err, store.ErrAlreadyExists) {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "tag key already exists")
		return
	}
	if err != nil {
		if strings.Contains(err.Error(), "parent") || strings.Contains(err.Error(), "required") {
			gcperrors.InvalidArgument(w, err.Error())
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, tagKeyJSON(k))
}

func (h *Handler) handleListTagKeys(w http.ResponseWriter, r *http.Request) {
	parent := r.URL.Query().Get("parent")
	if parent == "" {
		gcperrors.InvalidArgument(w, "parent query parameter is required")
		return
	}
	if _, ok := h.require(w, r, "resourcemanager.tagKeys.list", parent); !ok {
		return
	}
	list, err := h.Store.ListTagKeys(parent)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, k := range list {
		out = append(out, tagKeyJSON(k))
	}
	writeJSON(w, http.StatusOK, map[string]any{"tagKeys": out})
}

func (h *Handler) handleGetTagKey(w http.ResponseWriter, r *http.Request) {
	id, _ := splitColonAction(r.PathValue("tagKey"))
	resource := "tagKeys/" + id
	if _, ok := h.require(w, r, "resourcemanager.tagKeys.get", resource); !ok {
		return
	}
	k, ok, err := h.Store.GetTagKey(id)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "TagKey not found")
		return
	}
	writeJSON(w, http.StatusOK, tagKeyJSON(k))
}

func (h *Handler) handleDeleteTagKey(w http.ResponseWriter, r *http.Request) {
	id, _ := splitColonAction(r.PathValue("tagKey"))
	resource := "tagKeys/" + id
	if _, ok := h.require(w, r, "resourcemanager.tagKeys.delete", resource); !ok {
		return
	}
	ok, err := h.Store.DeleteTagKey(id)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "TagKey not found")
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("{}"))
}

func (h *Handler) handleCreateTagBinding(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		gcperrors.InvalidArgument(w, "unable to read body")
		return
	}
	var req struct {
		Parent                 string `json:"parent"`
		TagValue               string `json:"tagValue"`
		TagValueNamespacedName string `json:"tagValueNamespacedName"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	parent := strings.TrimSpace(req.Parent)
	if parent == "" {
		gcperrors.InvalidArgument(w, "parent is required")
		return
	}
	if _, ok := h.require(w, r, "resourcemanager.tagBindings.create", parent); !ok {
		return
	}
	ns := strings.TrimSpace(req.TagValueNamespacedName)
	if ns == "" {
		ns = strings.TrimSpace(req.TagValue)
	}
	if ns == "" {
		gcperrors.InvalidArgument(w, "tagValueNamespacedName (or tagValue) is required")
		return
	}
	b, err := h.Store.CreateTagBinding(parent, ns)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, tagBindingJSON(b))
}

func (h *Handler) handleListTagBindings(w http.ResponseWriter, r *http.Request) {
	parent := r.URL.Query().Get("parent")
	if parent == "" {
		gcperrors.InvalidArgument(w, "parent query parameter is required")
		return
	}
	if _, ok := h.require(w, r, "resourcemanager.tagBindings.list", parent); !ok {
		return
	}
	list, err := h.Store.ListTagBindings(parent)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, b := range list {
		out = append(out, tagBindingJSON(b))
	}
	writeJSON(w, http.StatusOK, map[string]any{"tagBindings": out})
}

func (h *Handler) handleGetTagBinding(w http.ResponseWriter, r *http.Request) {
	id, _ := splitColonAction(r.PathValue("tagBinding"))
	resource := "tagBindings/" + id
	if _, ok := h.require(w, r, "resourcemanager.tagBindings.get", resource); !ok {
		return
	}
	b, ok, err := h.Store.GetTagBinding(id)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "TagBinding not found")
		return
	}
	writeJSON(w, http.StatusOK, tagBindingJSON(b))
}

func (h *Handler) handleDeleteTagBinding(w http.ResponseWriter, r *http.Request) {
	id, _ := splitColonAction(r.PathValue("tagBinding"))
	resource := "tagBindings/" + id
	if _, ok := h.require(w, r, "resourcemanager.tagBindings.delete", resource); !ok {
		return
	}
	ok, err := h.Store.DeleteTagBinding(id)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "TagBinding not found")
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("{}"))
}

func tagKeyJSON(k store.TagKey) map[string]any {
	return map[string]any{
		"name":           k.Name,
		"parent":         k.Parent,
		"shortName":      k.ShortName,
		"namespacedName": k.NamespacedName,
		"description":    k.Description,
		"etag":           k.Etag,
		"createTime":     k.CreatedAt,
		"updateTime":     k.UpdatedAt,
	}
}

func tagBindingJSON(b store.TagBinding) map[string]any {
	return map[string]any{
		"name":                   b.Name,
		"parent":                 b.Parent,
		"tagValue":               b.TagValue,
		"tagValueNamespacedName": b.TagValueNamespacedName,
	}
}
