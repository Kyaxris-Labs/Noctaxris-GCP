package cloudrun

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/compute"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// DefaultLocation is the lab default Cloud Run location.
const DefaultLocation = "us-central1"

// Service serves Cloud Run Admin API v2 REST (lab subset).
// Invoker defaults to compute.MockInvoker; nested hooks activate when
// NOCTAXRIS_GCP_DOCKER_HOST is set (still no host docker.sock).
type Service struct {
	Store   *store.Store
	Authz   *authz.Evaluator
	Invoker compute.Invoker
}

func (s *Service) invoker() compute.Invoker {
	if s.Invoker != nil {
		return s.Invoker
	}
	return compute.NewInvokerFromEnv()
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// Mount registers Cloud Run v2 REST routes.
// Colon methods (:invoke, :getIamPolicy, :setIamPolicy) are parsed from the path segment.
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	mux.HandleFunc("GET /v2/projects/{project}/locations/{location}/services", s.wrap(principalFrom, s.listServices))
	mux.HandleFunc("POST /v2/projects/{project}/locations/{location}/services", s.wrap(principalFrom, s.createService))
	mux.HandleFunc("GET /v2/projects/{project}/locations/{location}/services/{service}", s.wrap(principalFrom, s.getOrInvoke))
	mux.HandleFunc("POST /v2/projects/{project}/locations/{location}/services/{service}", s.wrap(principalFrom, s.getOrInvoke))
	mux.HandleFunc("PATCH /v2/projects/{project}/locations/{location}/services/{service}", s.wrap(principalFrom, s.patchService))
	mux.HandleFunc("DELETE /v2/projects/{project}/locations/{location}/services/{service}", s.wrap(principalFrom, s.deleteService))
	mux.HandleFunc("GET /v2/projects/{project}/locations/{location}/services/{service}/revisions", s.wrap(principalFrom, s.listRevisions))

	mux.HandleFunc("GET /v2/projects/{project}/locations/{location}/jobs", s.wrap(principalFrom, s.listJobs))
	mux.HandleFunc("POST /v2/projects/{project}/locations/{location}/jobs", s.wrap(principalFrom, s.createJob))
	mux.HandleFunc("GET /v2/projects/{project}/locations/{location}/jobs/{job}", s.wrap(principalFrom, s.getJob))
	mux.HandleFunc("PATCH /v2/projects/{project}/locations/{location}/jobs/{job}", s.wrap(principalFrom, s.patchJob))
	mux.HandleFunc("DELETE /v2/projects/{project}/locations/{location}/jobs/{job}", s.wrap(principalFrom, s.deleteJob))
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

// requireAny allows when any listed resource (or its project parent chain) grants permission.
func (s *Service) requireAny(p authn.Principal, permission string, resources ...string) error {
	ok, err := s.Authz.EvaluateAny(p.Email, p.IsRoot, permission, resources...)
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

func jobName(project, location, id string) string {
	return fmt.Sprintf("projects/%s/locations/%s/jobs/%s", project, location, id)
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
	trafficJSON := ""
	if t, ok := body["traffic"]; ok {
		raw, _ := json.Marshal(t)
		trafficJSON = string(raw)
	}
	name := serviceName(project, location, serviceID)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	svc := store.RunService{
		Name:            name,
		ProjectID:       project,
		Location:        location,
		ServiceID:       serviceID,
		TemplateJSON:    string(tplRaw),
		LabResponseBody: labBody,
		TrafficJSON:     trafficJSON,
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
	writeDoneOperation(w, project, location, "create-"+serviceID, toServiceJSON(out))
}

func (s *Service) getOrInvoke(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	seg := r.PathValue("service")
	id, action := splitAction(seg)
	switch action {
	case "invoke":
		s.invoke(w, r, p, project, location, id)
		return
	case "getIamPolicy":
		s.getIamPolicy(w, r, p, project, location, id)
		return
	case "setIamPolicy":
		s.setIamPolicy(w, r, p, project, location, id)
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
	trafficJSON := ""
	if t, ok := body["traffic"]; ok {
		raw, _ := json.Marshal(t)
		trafficJSON = string(raw)
	}
	name := serviceName(project, location, id)
	if tplRaw == "" && labBody == "" && trafficJSON != "" {
		svc, ok, err := s.Store.SetRunServiceTraffic(name, trafficJSON)
		if err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		}
		if !ok {
			gcperrors.NotFound(w, "Service not found")
			return
		}
		writeDoneOperation(w, project, location, "update-"+id, toServiceJSON(svc))
		return
	}
	svc, ok, err := s.Store.UpdateRunService(name, tplRaw, labBody, trafficJSON)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Service not found")
		return
	}
	writeDoneOperation(w, project, location, "update-"+id, toServiceJSON(svc))
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
	svc, ok, err := s.Store.GetRunService(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
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
			"name":           rev.Name,
			"uid":            fmt.Sprintf("%s-%05d", svc.UID, rev.Generation),
			"createTime":     rev.CreatedAt,
			"generation":     strconv.FormatInt(rev.Generation, 10),
			"containers":     containersFromTemplate(tpl),
			"service":        name,
			"reconciling":    false,
			"observedGeneration": strconv.FormatInt(rev.Generation, 10),
			"conditions": []map[string]any{
				{"type": "Ready", "state": "CONDITION_SUCCEEDED", "message": "lab revision ready"},
			},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"revisions": items})
}

func (s *Service) getIamPolicy(w http.ResponseWriter, _ *http.Request, p authn.Principal, project, location, id string) {
	if err := s.require(p, "run.services.getIamPolicy", project); err != nil {
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
	raw, found, err := s.Store.GetIAMPolicyJSON(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !found {
		writeJSON(w, http.StatusOK, authz.Policy{Etag: "ACAB", Bindings: []authz.Binding{}})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func (s *Service) setIamPolicy(w http.ResponseWriter, r *http.Request, p authn.Principal, project, location, id string) {
	if err := s.require(p, "run.services.setIamPolicy", project); err != nil {
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
	var req struct {
		Policy authz.Policy `json:"policy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		gcperrors.InvalidArgument(w, "invalid setIamPolicy body")
		return
	}
	if err := s.Store.PutIAMPolicyJSON(name, req.Policy); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, req.Policy)
}

func (s *Service) createJob(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	if err := s.require(p, "run.jobs.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	jobID := r.URL.Query().Get("jobId")
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body == nil {
		body = map[string]any{}
	}
	if jobID == "" {
		if n, _ := body["name"].(string); n != "" {
			parts := strings.Split(n, "/")
			jobID = parts[len(parts)-1]
		}
	}
	if jobID == "" {
		gcperrors.InvalidArgument(w, "jobId is required")
		return
	}
	template, _ := body["template"].(map[string]any)
	if template == nil {
		template = map[string]any{}
	}
	tplRaw, _ := json.Marshal(template)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	name := jobName(project, location, jobID)
	created, err := s.Store.CreateRunJob(store.RunJob{
		Name: name, ProjectID: project, Location: location, JobID: jobID,
		TemplateJSON: string(tplRaw), CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "job already exists")
		return
	}
	out, _, _ := s.Store.GetRunJob(name)
	writeJSON(w, http.StatusOK, toJobJSON(out))
}

func (s *Service) getJob(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	id, _ := splitAction(r.PathValue("job"))
	if err := s.require(p, "run.jobs.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	job, ok, err := s.Store.GetRunJob(jobName(project, location, id))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Job not found")
		return
	}
	writeJSON(w, http.StatusOK, toJobJSON(job))
}

func (s *Service) listJobs(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	if err := s.require(p, "run.jobs.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	list, err := s.Store.ListRunJobs(project, location)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, job := range list {
		items = append(items, toJobJSON(job))
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": items})
}

func (s *Service) patchJob(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	id, _ := splitAction(r.PathValue("job"))
	if err := s.require(p, "run.jobs.update", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	tplRaw := ""
	if template, ok := body["template"].(map[string]any); ok {
		b, _ := json.Marshal(template)
		tplRaw = string(b)
	}
	job, ok, err := s.Store.UpdateRunJob(jobName(project, location, id), tplRaw)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Job not found")
		return
	}
	writeJSON(w, http.StatusOK, toJobJSON(job))
}

func (s *Service) deleteJob(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	id, _ := splitAction(r.PathValue("job"))
	if err := s.require(p, "run.jobs.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	ok, err := s.Store.DeleteRunJob(jobName(project, location, id))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Job not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Service) invoke(w http.ResponseWriter, r *http.Request, p authn.Principal, project, location, id string) {
	name := serviceName(project, location, id)
	// Project binding or service-resource Invoker (roles/run.invoker → run.routes.invoke).
	if err := s.requireAny(p, "run.routes.invoke", name, "projects/"+project); err != nil {
		writeAuthzErr(w, err)
		return
	}
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
		"query":     r.URL.RawQuery,
		"headers":   flattenHeaders(r.Header),
		"body":      string(body),
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
	}
	raw, _ := json.Marshal(rec)
	_ = s.Store.RecordRunInvoke(name, string(raw))

	env := envFromTemplateJSON(svc.TemplateJSON)
	respBody := []byte(svc.LabResponseBody)
	if len(respBody) == 0 {
		defaultJSON, _ := json.Marshal(map[string]any{
			"ok":      true,
			"service": name,
			"env":     env,
		})
		respBody = defaultJSON
	}
	status := labStatusFromTemplate(svc.TemplateJSON, env)
	delay := labDelayFromTemplate(svc.TemplateJSON, env)
	inv := s.invoker()
	// Deterministic labResponseBody fixtures stay mock-only (no nested overwrite).
	if svc.LabResponseBody != "" {
		inv = compute.MockInvoker{}
	}
	result, err := inv.Invoke(r.Context(), compute.InvokeRequest{
		ServiceName:  name,
		Method:       r.Method,
		Path:         r.URL.Path,
		Query:        r.URL.RawQuery,
		Headers:      flattenHeaders(r.Header),
		Body:         body,
		StatusCode:   status,
		Delay:        delay,
		ResponseBody: respBody,
		Image:        imageFromTemplateJSON(svc.TemplateJSON),
		Env:          env,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	for k, v := range result.Headers {
		w.Header().Set(k, v)
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	}
	code := result.StatusCode
	if code == 0 {
		code = http.StatusOK
	}
	w.WriteHeader(code)
	_, _ = w.Write(result.Body)
}

func toServiceJSON(svc store.RunService) map[string]any {
	var tpl any
	_ = json.Unmarshal([]byte(svc.TemplateJSON), &tpl)
	var traffic any
	if svc.TrafficJSON != "" {
		_ = json.Unmarshal([]byte(svc.TrafficJSON), &traffic)
	}
	if traffic == nil {
		traffic = []any{}
	}
	ready := map[string]any{
		"type":    "Ready",
		"state":   "CONDITION_SUCCEEDED",
		"message": "lab service ready",
	}
	return map[string]any{
		"name":                  svc.Name,
		"uid":                   svc.UID,
		"generation":            strconv.FormatInt(svc.Generation, 10),
		"createTime":            svc.CreatedAt,
		"updateTime":            svc.UpdatedAt,
		"uri":                   svc.URI,
		"latestReadyRevision":   svc.LatestRevision,
		"latestCreatedRevision": svc.LatestRevision,
		"observedGeneration":    strconv.FormatInt(svc.Generation, 10),
		"template":              tpl,
		"traffic":               traffic,
		"terminalCondition":     ready,
		"conditions":            []map[string]any{ready},
		"reconciling":           false,
	}
}

// writeDoneOperation returns a completed LRO so google_cloud_run_v2_service
// (OpAsync) does not treat the service resource name as an unfinished operation.
func writeDoneOperation(w http.ResponseWriter, project, location, opID string, response any) {
	writeJSON(w, http.StatusOK, map[string]any{
		"name":     fmt.Sprintf("projects/%s/locations/%s/operations/%s", project, location, opID),
		"done":     true,
		"response": response,
	})
}

func toJobJSON(job store.RunJob) map[string]any {
	var tpl any
	_ = json.Unmarshal([]byte(job.TemplateJSON), &tpl)
	return map[string]any{
		"name":       job.Name,
		"uid":        job.UID,
		"generation": strconv.FormatInt(job.Generation, 10),
		"createTime": job.CreatedAt,
		"updateTime": job.UpdatedAt,
		"template":   tpl,
		"reconciling": false,
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

func labStatusFromTemplate(templateJSON string, env map[string]string) int {
	var tpl map[string]any
	_ = json.Unmarshal([]byte(templateJSON), &tpl)
	if n, ok := asInt(tpl["labStatusCode"]); ok && n >= 100 && n <= 599 {
		return n
	}
	if v, ok := env["RESPONSE_STATUS"]; ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 100 && n <= 599 {
			return n
		}
	}
	return http.StatusOK
}

func labDelayFromTemplate(templateJSON string, env map[string]string) time.Duration {
	var tpl map[string]any
	_ = json.Unmarshal([]byte(templateJSON), &tpl)
	ms := 0
	if n, ok := asInt(tpl["labDelayMs"]); ok && n > 0 {
		ms = n
	} else if v, ok := env["RESPONSE_DELAY_MS"]; ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			ms = n
		}
	}
	if ms <= 0 {
		return 0
	}
	// Cap theatre delay so tests stay bounded.
	if ms > 5000 {
		ms = 5000
	}
	return time.Duration(ms) * time.Millisecond
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	case string:
		i, err := strconv.Atoi(n)
		return i, err == nil
	default:
		return 0, false
	}
}

func imageFromTemplateJSON(templateJSON string) string {
	var tpl map[string]any
	_ = json.Unmarshal([]byte(templateJSON), &tpl)
	containers, _ := tpl["containers"].([]any)
	for _, c := range containers {
		cm, _ := c.(map[string]any)
		if img, _ := cm["image"].(string); img != "" {
			return img
		}
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
