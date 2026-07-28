package cloudrun

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// DefaultLocation is the lab default Cloud Run location.
const DefaultLocation = "us-central1"

// Service serves Cloud Run Admin API v2 REST (lab subset, no container start).
type Service struct {
	Store *store.Store
	Authz *authz.Evaluator
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// Mount registers Cloud Run v2 REST routes.
// Colon methods (:invoke) are parsed from the service path segment.
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	mux.HandleFunc("GET /v2/projects/{project}/locations/{location}/services", s.wrap(principalFrom, s.listServices))
	mux.HandleFunc("POST /v2/projects/{project}/locations/{location}/services", s.wrap(principalFrom, s.createService))
	mux.HandleFunc("GET /v2/projects/{project}/locations/{location}/services/{service}", s.wrap(principalFrom, s.getOrInvoke))
	mux.HandleFunc("POST /v2/projects/{project}/locations/{location}/services/{service}", s.wrap(principalFrom, s.getOrInvoke))
	mux.HandleFunc("PATCH /v2/projects/{project}/locations/{location}/services/{service}", s.wrap(principalFrom, s.patchService))
	mux.HandleFunc("DELETE /v2/projects/{project}/locations/{location}/services/{service}", s.wrap(principalFrom, s.deleteService))
	mux.HandleFunc("GET /v2/projects/{project}/locations/{location}/services/{service}/revisions", s.wrap(principalFrom, s.listRevisions))
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

func splitAction(seg string) (name, action string) {
	if i := strings.IndexByte(seg, ':'); i >= 0 {
		return seg[:i], seg[i+1:]
	}
	return seg, ""
}

func serviceName(project, location, id string) string {
	return fmt.Sprintf("projects/%s/locations/%s/services/%s", project, location, id)
}

func (s *Service) createService(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	if err := s.require(p, "run.services.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	serviceID := r.URL.Query().Get("serviceId")
	if serviceID == "" {
		gcperrors.InvalidArgument(w, "serviceId is required")
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body == nil {
		body = map[string]any{}
	}
	template, _ := body["template"].(map[string]any)
	if template == nil {
		template = map[string]any{}
	}
	tplRaw, _ := json.Marshal(template)
	labBody := labResponseFromTemplate(template)
	name := serviceName(project, location, serviceID)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	svc := store.RunService{
		Name:            name,
		ProjectID:       project,
		Location:        location,
		ServiceID:       serviceID,
		TemplateJSON:    string(tplRaw),
		LabResponseBody: labBody,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	created, err := s.Store.CreateRunService(svc)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "service already exists")
		return
	}
	out, ok, err := s.Store.GetRunService(name)
	if err != nil || !ok {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "created service missing")
		return
	}
	writeJSON(w, http.StatusOK, toServiceJSON(out))
}

func (s *Service) getOrInvoke(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	seg := r.PathValue("service")
	id, action := splitAction(seg)
	if action == "invoke" {
		s.invoke(w, r, p, project, location, id)
		return
	}
	if r.Method != http.MethodGet {
		gcperrors.NotFound(w, "unknown Cloud Run method")
		return
	}
	if err := s.require(p, "run.services.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := serviceName(project, location, id)
	svc, ok, err := s.Store.GetRunService(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Service not found")
		return
	}
	writeJSON(w, http.StatusOK, toServiceJSON(svc))
}

func (s *Service) listServices(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	if err := s.require(p, "run.services.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	list, err := s.Store.ListRunServices(project, location)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, svc := range list {
		items = append(items, toServiceJSON(svc))
	}
	writeJSON(w, http.StatusOK, map[string]any{"services": items})
}

func (s *Service) patchService(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	id, _ := splitAction(r.PathValue("service"))
	if err := s.require(p, "run.services.update", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	template, _ := body["template"].(map[string]any)
	tplRaw := ""
	labBody := ""
	if template != nil {
		b, _ := json.Marshal(template)
		tplRaw = string(b)
		labBody = labResponseFromTemplate(template)
	}
	name := serviceName(project, location, id)
	svc, ok, err := s.Store.UpdateRunService(name, tplRaw, labBody)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Service not found")
		return
	}
	writeJSON(w, http.StatusOK, toServiceJSON(svc))
}

func (s *Service) deleteService(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	id, _ := splitAction(r.PathValue("service"))
	if err := s.require(p, "run.services.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := serviceName(project, location, id)
	ok, err := s.Store.DeleteRunService(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Service not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Service) listRevisions(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	id, _ := splitAction(r.PathValue("service"))
	if err := s.require(p, "run.services.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := serviceName(project, location, id)
	if _, ok, err := s.Store.GetRunService(name); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "Service not found")
		return
	}
	revs, err := s.Store.ListRunRevisions(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(revs))
	for _, rev := range revs {
		var tpl any
		_ = json.Unmarshal([]byte(rev.TemplateJSON), &tpl)
		items = append(items, map[string]any{
			"name":       rev.Name,
			"createTime": rev.CreatedAt,
			"generation": strconv.FormatInt(rev.Generation, 10),
			"containers": containersFromTemplate(tpl),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"revisions": items})
}

func (s *Service) invoke(w http.ResponseWriter, r *http.Request, p authn.Principal, project, location, id string) {
	if err := s.require(p, "run.routes.invoke", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := serviceName(project, location, id)
	svc, ok, err := s.Store.GetRunService(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Service not found")
		return
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	rec := map[string]any{
		"method":    r.Method,
		"path":      r.URL.Path,
		"headers":   flattenHeaders(r.Header),
		"body":      string(body),
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
	}
	raw, _ := json.Marshal(rec)
	_ = s.Store.RecordRunInvoke(name, string(raw))

	if svc.LabResponseBody != "" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(svc.LabResponseBody))
		return
	}
	env := envFromTemplateJSON(svc.TemplateJSON)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"service": name,
		"env":     env,
	})
}

func toServiceJSON(svc store.RunService) map[string]any {
	var tpl any
	_ = json.Unmarshal([]byte(svc.TemplateJSON), &tpl)
	return map[string]any{
		"name":                  svc.Name,
		"uid":                   svc.UID,
		"generation":            strconv.FormatInt(svc.Generation, 10),
		"createTime":            svc.CreatedAt,
		"updateTime":            svc.UpdatedAt,
		"uri":                   svc.URI,
		"latestReadyRevision":   svc.LatestRevision,
		"latestCreatedRevision": svc.LatestRevision,
		"template":              tpl,
		"reconciling":           false,
	}
}

func labResponseFromTemplate(template map[string]any) string {
	if v, ok := template["labResponseBody"].(string); ok && v != "" {
		return v
	}
	env := envMapFromTemplate(template)
	if v, ok := env["RESPONSE_BODY"]; ok && v != "" {
		return v
	}
	return ""
}

func envFromTemplateJSON(templateJSON string) map[string]string {
	var tpl map[string]any
	_ = json.Unmarshal([]byte(templateJSON), &tpl)
	return envMapFromTemplate(tpl)
}

func envMapFromTemplate(template map[string]any) map[string]string {
	out := map[string]string{}
	containers, _ := template["containers"].([]any)
	for _, c := range containers {
		cm, _ := c.(map[string]any)
		envs, _ := cm["env"].([]any)
		for _, e := range envs {
			em, _ := e.(map[string]any)
			name, _ := em["name"].(string)
			val, _ := em["value"].(string)
			if name != "" {
				out[name] = val
			}
		}
	}
	return out
}

func containersFromTemplate(tpl any) any {
	m, _ := tpl.(map[string]any)
	if m == nil {
		return []any{}
	}
	if c, ok := m["containers"]; ok {
		return c
	}
	return []any{}
}

func flattenHeaders(h http.Header) map[string]string {
	out := map[string]string{}
	for k, vals := range h {
		if strings.EqualFold(k, "Authorization") {
			continue
		}
		out[k] = strings.Join(vals, ",")
	}
	return out
}
