package cloudtasks

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/httpegress"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/labtoken"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
	"github.com/google/uuid"
)

// DefaultLocation is the lab default Cloud Tasks location.
const DefaultLocation = "us-central1"

// Service serves Cloud Tasks v2 REST (queues/tasks CRUD + :run dispatch).
type Service struct {
	Store *store.Store
	Authz *authz.Evaluator

	// HTTPClient overrides the default httpegress client (tests / in-process delivery).
	HTTPClient *http.Client

	client *http.Client
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// Mount registers Cloud Tasks v2 REST routes.
// Colon methods (:run) are parsed from the task path segment.
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	if s.HTTPClient != nil {
		s.client = s.HTTPClient
	} else if s.client == nil {
		s.client = httpegress.Client(5 * time.Second)
	}
	mux.HandleFunc("GET /v2/projects/{project}/locations/{location}/queues", s.wrap(principalFrom, s.listQueues))
	mux.HandleFunc("POST /v2/projects/{project}/locations/{location}/queues", s.wrap(principalFrom, s.createQueue))
	mux.HandleFunc("GET /v2/projects/{project}/locations/{location}/queues/{queue}", s.wrap(principalFrom, s.getQueue))
	mux.HandleFunc("PATCH /v2/projects/{project}/locations/{location}/queues/{queue}", s.wrap(principalFrom, s.patchQueue))
	mux.HandleFunc("DELETE /v2/projects/{project}/locations/{location}/queues/{queue}", s.wrap(principalFrom, s.deleteQueue))
	mux.HandleFunc("GET /v2/projects/{project}/locations/{location}/queues/{queue}/tasks", s.wrap(principalFrom, s.listTasks))
	mux.HandleFunc("POST /v2/projects/{project}/locations/{location}/queues/{queue}/tasks", s.wrap(principalFrom, s.createTask))
	mux.HandleFunc("GET /v2/projects/{project}/locations/{location}/queues/{queue}/tasks/{task}", s.wrap(principalFrom, s.getTask))
	mux.HandleFunc("DELETE /v2/projects/{project}/locations/{location}/queues/{queue}/tasks/{task}", s.wrap(principalFrom, s.deleteTask))
	mux.HandleFunc("POST /v2/projects/{project}/locations/{location}/queues/{queue}/tasks/{task}", s.wrap(principalFrom, s.runTaskOrUnknown))
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

func queueName(project, location, id string) string {
	return fmt.Sprintf("projects/%s/locations/%s/queues/%s", project, location, id)
}

func (s *Service) createQueue(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	if err := s.require(p, "cloudtasks.queues.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	queueID := r.URL.Query().Get("queueId")
	if queueID == "" {
		if n, _ := body["name"].(string); n != "" {
			parts := strings.Split(n, "/")
			queueID = parts[len(parts)-1]
		}
	}
	if queueID == "" {
		gcperrors.InvalidArgument(w, "queueId is required")
		return
	}
	name := queueName(project, location, queueID)
	rateLimitsJSON := `{"maxDispatchesPerSecond":500,"maxBurstSize":100,"maxConcurrentDispatches":1000}`
	retryConfigJSON := `{"maxAttempts":100,"maxRetryDuration":"0s","minBackoff":"0.100s","maxBackoff":"3600s","maxDoublings":16}`
	appEngineRouting := ""
	if rl, ok := body["rateLimits"]; ok {
		raw, _ := json.Marshal(rl)
		rateLimitsJSON = string(raw)
	}
	if rc, ok := body["retryConfig"]; ok {
		raw, _ := json.Marshal(rc)
		retryConfigJSON = string(raw)
	}
	if ae, ok := body["appEngineRoutingOverride"]; ok {
		raw, _ := json.Marshal(ae)
		appEngineRouting = string(raw)
	}
	created, err := s.Store.CreateCloudTasksQueue(store.CloudTasksQueue{
		Name: name, ProjectID: project, Location: location, QueueID: queueID, State: "RUNNING",
		RateLimitsJSON: rateLimitsJSON, RetryConfigJSON: retryConfigJSON, AppEngineRoutingOverrideJSON: appEngineRouting,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "queue already exists")
		return
	}
	q, _, _ := s.Store.GetCloudTasksQueue(name)
	writeJSON(w, http.StatusOK, toQueueJSON(q))
}

func (s *Service) getQueue(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	id, _ := splitAction(r.PathValue("queue"))
	if err := s.require(p, "cloudtasks.queues.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := queueName(project, location, id)
	q, ok, err := s.Store.GetCloudTasksQueue(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Queue not found")
		return
	}
	writeJSON(w, http.StatusOK, toQueueJSON(q))
}

func (s *Service) listQueues(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	if err := s.require(p, "cloudtasks.queues.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	list, err := s.Store.ListCloudTasksQueues(project, location)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, q := range list {
		items = append(items, toQueueJSON(q))
	}
	writeJSON(w, http.StatusOK, map[string]any{"queues": items})
}

func (s *Service) patchQueue(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	id, _ := splitAction(r.PathValue("queue"))
	if err := s.require(p, "cloudtasks.queues.update", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := queueName(project, location, id)
	existing, ok, err := s.Store.GetCloudTasksQueue(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Queue not found")
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	if st, ok := body["state"].(string); ok && st != "" {
		existing.State = st
	}
	if rl, ok := body["rateLimits"]; ok {
		raw, _ := json.Marshal(rl)
		existing.RateLimitsJSON = string(raw)
	}
	if rc, ok := body["retryConfig"]; ok {
		raw, _ := json.Marshal(rc)
		existing.RetryConfigJSON = string(raw)
	}
	if ae, ok := body["appEngineRoutingOverride"]; ok {
		raw, _ := json.Marshal(ae)
		existing.AppEngineRoutingOverrideJSON = string(raw)
	}
	if _, err := s.Store.UpdateCloudTasksQueue(existing); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	out, _, _ := s.Store.GetCloudTasksQueue(name)
	writeJSON(w, http.StatusOK, toQueueJSON(out))
}

func (s *Service) deleteQueue(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	id, _ := splitAction(r.PathValue("queue"))
	if err := s.require(p, "cloudtasks.queues.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := queueName(project, location, id)
	ok, err := s.Store.DeleteCloudTasksQueue(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Queue not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Service) createTask(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	queueID, _ := splitAction(r.PathValue("queue"))
	if err := s.require(p, "cloudtasks.tasks.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	qName := queueName(project, location, queueID)
	if _, ok, err := s.Store.GetCloudTasksQueue(qName); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "Queue not found")
		return
	}
	var body struct {
		Task struct {
			Name                   string          `json:"name"`
			ScheduleTime           string          `json:"scheduleTime"`
			HTTPRequest            json.RawMessage `json:"httpRequest"`
			AppEngineHTTPRequest   json.RawMessage `json:"appEngineHttpRequest"`
		} `json:"task"`
		TaskID string `json:"taskId"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	taskID := body.TaskID
	if taskID == "" && body.Task.Name != "" {
		parts := strings.Split(body.Task.Name, "/")
		taskID = parts[len(parts)-1]
	}
	if taskID == "" {
		taskID = uuid.NewString()
	}
	name := qName + "/tasks/" + taskID
	now := time.Now().UTC().Format(time.RFC3339Nano)
	sched := body.Task.ScheduleTime
	if sched == "" {
		sched = now
	}
	httpJSON := string(body.Task.HTTPRequest)
	appEngineJSON := string(body.Task.AppEngineHTTPRequest)
	if httpJSON != "" && httpJSON != "null" {
		var hr struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal([]byte(httpJSON), &hr); err == nil && strings.TrimSpace(hr.URL) != "" {
			if err := httpegress.Validate(hr.URL); err != nil {
				gcperrors.InvalidArgument(w, err.Error())
				return
			}
		}
	}
	created, err := s.Store.CreateCloudTask(store.CloudTask{
		Name: name, QueueName: qName, ScheduleTime: sched, CreateTime: now,
		HTTPRequestJSON: httpJSON, AppEngineHTTPRequestJSON: appEngineJSON,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "task already exists")
		return
	}
	task, ok, err := s.Store.GetCloudTask(name)
	if err != nil || !ok {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "created task missing")
		return
	}
	if t, err := time.Parse(time.RFC3339Nano, sched); err == nil {
		if !t.After(time.Now().UTC().Add(2 * time.Second)) {
			s.dispatchHTTP(task)
			task, ok, err = s.Store.GetCloudTask(name)
			if err != nil || !ok {
				gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "task missing after dispatch")
				return
			}
		}
	} else if t, err := time.Parse(time.RFC3339, sched); err == nil {
		if !t.After(time.Now().UTC().Add(2 * time.Second)) {
			s.dispatchHTTP(task)
			task, ok, err = s.Store.GetCloudTask(name)
			if err != nil || !ok {
				gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "task missing after dispatch")
				return
			}
		}
	}
	writeJSON(w, http.StatusOK, toTaskJSON(task))
}

func (s *Service) listTasks(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	queueID, _ := splitAction(r.PathValue("queue"))
	if err := s.require(p, "cloudtasks.tasks.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	qName := queueName(project, location, queueID)
	list, err := s.Store.ListCloudTasks(qName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, t := range list {
		items = append(items, toTaskJSON(t))
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": items})
}

func (s *Service) getTask(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	queueID, _ := splitAction(r.PathValue("queue"))
	taskID, _ := splitAction(r.PathValue("task"))
	if err := s.require(p, "cloudtasks.tasks.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := queueName(project, location, queueID) + "/tasks/" + taskID
	task, ok, err := s.Store.GetCloudTask(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Task not found")
		return
	}
	writeJSON(w, http.StatusOK, toTaskJSON(task))
}

func (s *Service) deleteTask(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	queueID, _ := splitAction(r.PathValue("queue"))
	taskID, _ := splitAction(r.PathValue("task"))
	if err := s.require(p, "cloudtasks.tasks.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := queueName(project, location, queueID) + "/tasks/" + taskID
	ok, err := s.Store.DeleteCloudTask(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Task not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Service) runTaskOrUnknown(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	queueID, _ := splitAction(r.PathValue("queue"))
	taskID, action := splitAction(r.PathValue("task"))
	if action != "run" {
		gcperrors.NotFound(w, "unknown Cloud Tasks method")
		return
	}
	if err := s.require(p, "cloudtasks.tasks.run", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := queueName(project, location, queueID) + "/tasks/" + taskID
	task, ok, err := s.Store.GetCloudTask(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Task not found")
		return
	}
	s.dispatchHTTP(task)
	out, _, _ := s.Store.GetCloudTask(name)
	writeJSON(w, http.StatusOK, toTaskJSON(out))
}

func (s *Service) dispatchHTTP(task store.CloudTask) {
	_, _, _ = s.Store.IncrementCloudTaskDispatch(task.Name)
	defer func() { _ = s.Store.IncrementCloudTaskResponse(task.Name) }()
	if task.HTTPRequestJSON == "" {
		// App Engine HTTP theatre: no remote dispatch; counts still bump.
		return
	}
	var hr struct {
		URL        string            `json:"url"`
		HttpMethod string            `json:"httpMethod"`
		Headers    map[string]string `json:"headers"`
		Body       string            `json:"body"`
	}
	if err := json.Unmarshal([]byte(task.HTTPRequestJSON), &hr); err != nil || hr.URL == "" {
		return
	}
	if err := httpegress.Validate(hr.URL); err != nil {
		return
	}
	method := hr.HttpMethod
	if method == "" {
		method = http.MethodPost
	}
	var body []byte
	if hr.Body != "" {
		if decoded, err := base64.StdEncoding.DecodeString(hr.Body); err == nil {
			body = decoded
		} else {
			body = []byte(hr.Body)
		}
	}
	// Lab catcher: record without outbound HTTP (same-process theatre).
	if u, perr := url.Parse(hr.URL); perr == nil && httpegress.IsLabCatcher(u, strings.ToLower(u.Scheme)) {
		store.RecordHTTPCatcher(string(body))
		return
	}
	req, err := http.NewRequest(method, hr.URL, bytes.NewReader(body))
	if err != nil {
		return
	}
	for k, v := range hr.Headers {
		req.Header.Set(k, v)
	}
	if email := store.HTTPAuthServiceAccountEmail(task.HTTPRequestJSON); email != "" {
		projectID := projectFromQueueName(task.QueueName)
		_ = s.Store.EnsureServiceAccount(projectID, email, "cloud tasks dispatch SA")
		if tok, _, merr := labtoken.Mint(s.Store, email, labtoken.DefaultLifetime); merr == nil {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}
	resp, err := s.client.Do(req)
	if err == nil {
		_ = resp.Body.Close()
	}
}

func projectFromQueueName(queueName string) string {
	// projects/{project}/locations/{loc}/queues/{q}
	parts := strings.Split(queueName, "/")
	if len(parts) >= 2 && parts[0] == "projects" {
		return parts[1]
	}
	return ""
}

func toQueueJSON(q store.CloudTasksQueue) map[string]any {
	out := map[string]any{
		"name":  q.Name,
		"state": q.State,
	}
	if q.RateLimitsJSON != "" {
		var rl any
		_ = json.Unmarshal([]byte(q.RateLimitsJSON), &rl)
		out["rateLimits"] = rl
	}
	if q.RetryConfigJSON != "" {
		var rc any
		_ = json.Unmarshal([]byte(q.RetryConfigJSON), &rc)
		out["retryConfig"] = rc
	}
	if q.AppEngineRoutingOverrideJSON != "" {
		var ae any
		_ = json.Unmarshal([]byte(q.AppEngineRoutingOverrideJSON), &ae)
		out["appEngineRoutingOverride"] = ae
	}
	return out
}

func toTaskJSON(t store.CloudTask) map[string]any {
	out := map[string]any{
		"name":          t.Name,
		"scheduleTime":  t.ScheduleTime,
		"createTime":    t.CreateTime,
		"dispatchCount": t.DispatchCount,
		"responseCount": t.ResponseCount,
		"view":          "BASIC",
	}
	if t.HTTPRequestJSON != "" {
		var hr any
		_ = json.Unmarshal([]byte(t.HTTPRequestJSON), &hr)
		out["httpRequest"] = hr
	}
	if t.AppEngineHTTPRequestJSON != "" {
		var ae any
		_ = json.Unmarshal([]byte(t.AppEngineHTTPRequestJSON), &ae)
		out["appEngineHttpRequest"] = ae
	}
	return out
}
