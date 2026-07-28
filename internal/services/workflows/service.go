package workflows

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
	"github.com/google/uuid"
)

// DefaultLocation is the lab default Workflows location.
const DefaultLocation = "us-central1"

// Service serves Cloud Workflows REST v1 (workflows CRUD + executions theatre).
type Service struct {
	Store *store.Store
	Authz *authz.Evaluator
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// Mount registers Workflows and Workflow Executions REST routes.
// Colon methods on workflow/execution segments are parsed via splitAction.
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/workflows", s.wrap(principalFrom, s.listWorkflows))
	mux.HandleFunc("POST /v1/projects/{project}/locations/{location}/workflows", s.wrap(principalFrom, s.createWorkflow))
	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/workflows/{workflow}", s.wrap(principalFrom, s.getWorkflow))
	mux.HandleFunc("PATCH /v1/projects/{project}/locations/{location}/workflows/{workflow}", s.wrap(principalFrom, s.patchWorkflow))
	mux.HandleFunc("DELETE /v1/projects/{project}/locations/{location}/workflows/{workflow}", s.wrap(principalFrom, s.deleteWorkflow))

	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/workflows/{workflow}/executions", s.wrap(principalFrom, s.listExecutions))
	mux.HandleFunc("POST /v1/projects/{project}/locations/{location}/workflows/{workflow}/executions", s.wrap(principalFrom, s.createExecution))
	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/workflows/{workflow}/executions/{execution}", s.wrap(principalFrom, s.getExecution))
	mux.HandleFunc("POST /v1/projects/{project}/locations/{location}/workflows/{workflow}/executions/{execution}", s.wrap(principalFrom, s.executionPOSTAction))
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

func workflowName(project, location, id string) string {
	return fmt.Sprintf("projects/%s/locations/%s/workflows/%s", project, location, id)
}

func (s *Service) createWorkflow(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	if err := s.require(p, "workflows.workflows.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	workflowID := r.URL.Query().Get("workflowId")
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body == nil {
		body = map[string]any{}
	}
	if workflowID == "" {
		if n, _ := body["name"].(string); n != "" {
			parts := strings.Split(n, "/")
			workflowID = parts[len(parts)-1]
		}
	}
	if workflowID == "" {
		gcperrors.InvalidArgument(w, "workflowId is required")
		return
	}
	source, _ := body["sourceContents"].(string)
	desc, _ := body["description"].(string)
	sa, _ := body["serviceAccount"].(string)
	labelsJSON := "{}"
	if labels, ok := body["labels"]; ok {
		raw, _ := json.Marshal(labels)
		labelsJSON = string(raw)
	}
	name := workflowName(project, location, workflowID)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	created, err := s.Store.CreateWorkflow(store.Workflow{
		Name: name, ProjectID: project, Location: location, WorkflowID: workflowID,
		Description: desc, SourceContents: source, ServiceAccount: sa,
		LabelsJSON: labelsJSON, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "workflow already exists")
		return
	}
	out, ok, err := s.Store.GetWorkflow(name)
	if err != nil || !ok {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "created workflow missing")
		return
	}
	writeJSON(w, http.StatusOK, toWorkflowJSON(out))
}

func (s *Service) getWorkflow(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	id, action := splitAction(r.PathValue("workflow"))
	if action != "" {
		gcperrors.NotFound(w, "unknown Workflows method")
		return
	}
	if err := s.require(p, "workflows.workflows.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	wf, ok, err := s.Store.GetWorkflow(workflowName(project, location, id))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Workflow not found")
		return
	}
	writeJSON(w, http.StatusOK, toWorkflowJSON(wf))
}

func (s *Service) patchWorkflow(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	id, _ := splitAction(r.PathValue("workflow"))
	if err := s.require(p, "workflows.workflows.update", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body == nil {
		body = map[string]any{}
	}
	mask := r.URL.Query().Get("updateMask")
	var descPtr, srcPtr, saPtr, labelsPtr *string
	if mask == "" || fieldMaskHas(mask, "description") {
		if v, ok := body["description"].(string); ok {
			descPtr = &v
		}
	}
	if mask == "" || fieldMaskHas(mask, "sourceContents") {
		if v, ok := body["sourceContents"].(string); ok {
			srcPtr = &v
		}
	}
	if mask == "" || fieldMaskHas(mask, "serviceAccount") {
		if v, ok := body["serviceAccount"].(string); ok {
			saPtr = &v
		}
	}
	if mask == "" || fieldMaskHas(mask, "labels") {
		if labels, ok := body["labels"]; ok {
			raw, _ := json.Marshal(labels)
			s := string(raw)
			labelsPtr = &s
		}
	}
	wf, ok, err := s.Store.PatchWorkflowDeepen(workflowName(project, location, id), descPtr, srcPtr, saPtr, labelsPtr)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Workflow not found")
		return
	}
	// Lab: return Workflow directly (real API returns a long-running Operation).
	writeJSON(w, http.StatusOK, toWorkflowJSON(wf))
}

func fieldMaskHas(mask, field string) bool {
	for _, p := range strings.Split(mask, ",") {
		if strings.TrimSpace(p) == field {
			return true
		}
	}
	return false
}

func (s *Service) listWorkflows(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	if err := s.require(p, "workflows.workflows.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	pageSize := parsePageSize(r.URL.Query().Get("pageSize"))
	pageToken := r.URL.Query().Get("pageToken")
	list, next, err := s.Store.ListWorkflowsPageDeepen(project, location, pageSize, pageToken)
	if err != nil {
		if strings.Contains(err.Error(), "invalid pageToken") {
			gcperrors.InvalidArgument(w, "invalid pageToken")
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, wf := range list {
		items = append(items, toWorkflowJSON(wf))
	}
	resp := map[string]any{"workflows": items}
	if next != "" {
		resp["nextPageToken"] = next
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Service) deleteWorkflow(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	id, _ := splitAction(r.PathValue("workflow"))
	if err := s.require(p, "workflows.workflows.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	ok, err := s.Store.DeleteWorkflow(workflowName(project, location, id))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Workflow not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Service) createExecution(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	wfID, _ := splitAction(r.PathValue("workflow"))
	if err := s.require(p, "workflows.executions.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	wfName := workflowName(project, location, wfID)
	wf, ok, err := s.Store.GetWorkflow(wfName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Workflow not found")
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body == nil {
		body = map[string]any{}
	}
	argument, _ := body["argument"].(string)
	if argument != "" {
		trimmed := strings.TrimSpace(argument)
		if !json.Valid([]byte(trimmed)) {
			gcperrors.InvalidArgument(w, "argument must be valid JSON")
			return
		}
		argument = trimmed
	}
	execID := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	// Theatre: start ACTIVE; get advances to SUCCEEDED; :cancel marks CANCELLED.
	name := wfName + "/executions/" + execID
	created, err := s.Store.CreateWorkflowExecution(store.WorkflowExecution{
		Name: name, WorkflowName: wfName, ProjectID: project, Location: location, WorkflowID: wfID,
		ExecutionID: execID, Argument: argument, Result: "", State: "ACTIVE",
		WorkflowRevisionID: wf.RevisionID, CreatedAt: now, StartTime: now, EndTime: "",
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "execution already exists")
		return
	}
	out, _, _ := s.Store.GetWorkflowExecution(name)
	writeJSON(w, http.StatusOK, toExecutionJSON(out))
}

func (s *Service) getExecution(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	wfID, _ := splitAction(r.PathValue("workflow"))
	execID, action := splitAction(r.PathValue("execution"))
	if action != "" {
		gcperrors.NotFound(w, "unknown Workflows method")
		return
	}
	if err := s.require(p, "workflows.executions.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := workflowName(project, location, wfID) + "/executions/" + execID
	ex, ok, err := s.Store.AdvanceWorkflowExecutionDeepen(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Execution not found")
		return
	}
	writeJSON(w, http.StatusOK, toExecutionJSON(ex))
}

func (s *Service) executionPOSTAction(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	wfID, _ := splitAction(r.PathValue("workflow"))
	execID, action := splitAction(r.PathValue("execution"))
	switch action {
	case "cancel":
		s.cancelExecution(w, r, p, project, location, wfID, execID)
	default:
		gcperrors.NotFound(w, "unknown Workflows method")
	}
}

func (s *Service) cancelExecution(w http.ResponseWriter, _ *http.Request, p authn.Principal, project, location, wfID, execID string) {
	if err := s.require(p, "workflows.executions.cancel", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := workflowName(project, location, wfID) + "/executions/" + execID
	ex, ok, err := s.Store.CancelWorkflowExecutionDeepen(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Execution not found")
		return
	}
	if ex.State != "CANCELLED" {
		gcperrors.WriteREST(w, http.StatusBadRequest, gcperrors.StatusFailedPrecondition,
			"execution is not ACTIVE")
		return
	}
	writeJSON(w, http.StatusOK, toExecutionJSON(ex))
}

func (s *Service) listExecutions(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	wfID, _ := splitAction(r.PathValue("workflow"))
	if err := s.require(p, "workflows.executions.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	wfName := workflowName(project, location, wfID)
	if _, ok, err := s.Store.GetWorkflow(wfName); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "Workflow not found")
		return
	}
	pageSize := parsePageSize(r.URL.Query().Get("pageSize"))
	pageToken := r.URL.Query().Get("pageToken")
	list, next, err := s.Store.ListWorkflowExecutionsPageDeepen(wfName, pageSize, pageToken)
	if err != nil {
		if strings.Contains(err.Error(), "invalid pageToken") {
			gcperrors.InvalidArgument(w, "invalid pageToken")
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, ex := range list {
		items = append(items, toExecutionJSON(ex))
	}
	resp := map[string]any{"executions": items}
	if next != "" {
		resp["nextPageToken"] = next
	}
	writeJSON(w, http.StatusOK, resp)
}

func parsePageSize(raw string) int {
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func toWorkflowJSON(w store.Workflow) map[string]any {
	var labels any = map[string]string{}
	_ = json.Unmarshal([]byte(w.LabelsJSON), &labels)
	return map[string]any{
		"name":               w.Name,
		"description":        w.Description,
		"state":              w.State,
		"revisionId":         w.RevisionID,
		"createTime":         w.CreatedAt,
		"updateTime":         w.UpdatedAt,
		"revisionCreateTime": w.UpdatedAt,
		"sourceContents":     w.SourceContents,
		"serviceAccount":     w.ServiceAccount,
		"labels":             labels,
	}
}

func toExecutionJSON(e store.WorkflowExecution) map[string]any {
	out := map[string]any{
		"name":               e.Name,
		"createTime":         e.CreatedAt,
		"startTime":          e.StartTime,
		"duration":           "0s",
		"state":              e.State,
		"argument":           e.Argument,
		"result":             e.Result,
		"workflowRevisionId": e.WorkflowRevisionID,
	}
	if e.EndTime != "" {
		out["endTime"] = e.EndTime
	}
	return out
}
