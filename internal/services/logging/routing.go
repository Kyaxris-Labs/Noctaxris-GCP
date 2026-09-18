package logging

import (
	"encoding/json"
	"net/http"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func (s *Service) listExclusions(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "logging.exclusions.list", project); err != nil {
		writeAuthz(w, err)
		return
	}
	list, err := s.Store.ListLogExclusions(project)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for i := range list {
		items = append(items, exclusionResource(&list[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"exclusions": items})
}

func (s *Service) createExclusion(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "logging.exclusions.create", project); err != nil {
		writeAuthz(w, err)
		return
	}
	id := r.URL.Query().Get("exclusionId")
	var body store.LogExclusion
	_ = json.NewDecoder(r.Body).Decode(&body)
	if id == "" {
		id = body.ExclusionID
	}
	if id == "" {
		gcperrors.InvalidArgument(w, "exclusionId is required")
		return
	}
	body.ProjectID = project
	body.ExclusionID = id
	ex, created, err := s.Store.CreateLogExclusion(body)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "exclusion already exists")
		return
	}
	writeJSON(w, http.StatusOK, exclusionResource(ex))
}

func (s *Service) getExclusion(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "logging.exclusions.get", project); err != nil {
		writeAuthz(w, err)
		return
	}
	name := "projects/" + project + "/exclusions/" + r.PathValue("exclusion")
	ex, ok, err := s.Store.GetLogExclusion(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "exclusion not found")
		return
	}
	writeJSON(w, http.StatusOK, exclusionResource(ex))
}

func (s *Service) deleteExclusion(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "logging.exclusions.delete", project); err != nil {
		writeAuthz(w, err)
		return
	}
	ok, err := s.Store.DeleteLogExclusion("projects/" + project + "/exclusions/" + r.PathValue("exclusion"))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "exclusion not found")
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Service) listBuckets(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "logging.buckets.list", project); err != nil {
		writeAuthz(w, err)
		return
	}
	_ = s.Store.EnsureDefaultLogRouting(project)
	list, err := s.Store.ListLogBuckets(project, r.PathValue("location"))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for i := range list {
		items = append(items, bucketResource(&list[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"buckets": items})
}

func (s *Service) getBucket(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "logging.buckets.get", project); err != nil {
		writeAuthz(w, err)
		return
	}
	_ = s.Store.EnsureDefaultLogRouting(project)
	name := "projects/" + project + "/locations/" + r.PathValue("location") + "/buckets/" + r.PathValue("bucket")
	b, ok, err := s.Store.GetLogBucket(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "bucket not found")
		return
	}
	writeJSON(w, http.StatusOK, bucketResource(b))
}

func (s *Service) listViews(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "logging.views.list", project); err != nil {
		writeAuthz(w, err)
		return
	}
	parent := "projects/" + project + "/locations/" + r.PathValue("location") + "/buckets/" + r.PathValue("bucket")
	list, err := s.Store.ListLogViews(parent)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for i := range list {
		items = append(items, viewResource(&list[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"views": items})
}

func (s *Service) createView(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "logging.views.create", project); err != nil {
		writeAuthz(w, err)
		return
	}
	viewID := r.URL.Query().Get("viewId")
	var body store.LogView
	_ = json.NewDecoder(r.Body).Decode(&body)
	if viewID == "" {
		viewID = body.ViewID
	}
	if viewID == "" {
		gcperrors.InvalidArgument(w, "viewId is required")
		return
	}
	body.ProjectID = project
	body.Location = r.PathValue("location")
	body.BucketID = r.PathValue("bucket")
	body.ViewID = viewID
	v, created, err := s.Store.CreateLogView(body)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "view already exists")
		return
	}
	writeJSON(w, http.StatusOK, viewResource(v))
}

func (s *Service) getView(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "logging.views.get", project); err != nil {
		writeAuthz(w, err)
		return
	}
	name := "projects/" + project + "/locations/" + r.PathValue("location") + "/buckets/" + r.PathValue("bucket") + "/views/" + r.PathValue("view")
	v, ok, err := s.Store.GetLogView(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "view not found")
		return
	}
	writeJSON(w, http.StatusOK, viewResource(v))
}

func exclusionResource(e *store.LogExclusion) map[string]any {
	return map[string]any{
		"name":        e.Name,
		"description": e.Description,
		"filter":      e.Filter,
		"disabled":    e.Disabled,
		"createTime":  e.CreatedAt,
	}
}

func bucketResource(b *store.LogBucket) map[string]any {
	return map[string]any{
		"name":           b.Name,
		"description":    b.Description,
		"lifecycleState": "ACTIVE",
		"createTime":     b.CreatedAt,
	}
}

func viewResource(v *store.LogView) map[string]any {
	return map[string]any{
		"name":       v.Name,
		"filter":     v.Filter,
		"createTime": v.CreatedAt,
	}
}
