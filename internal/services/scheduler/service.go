package scheduler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/httpegress"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// DefaultLocation is the lab default Scheduler location.
const DefaultLocation = "us-central1"

// Service serves Cloud Scheduler v1 REST (jobs CRUD + :run + best-effort ticker).
type Service struct {
	Store *store.Store
	Authz *authz.Evaluator

	mu      sync.Mutex
	tickers map[string]context.CancelFunc
	client  *http.Client
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// Mount registers Cloud Scheduler v1 REST routes.
// Colon methods (:run, :pause, :resume) are parsed from the job path segment.
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	if s.tickers == nil {
		s.tickers = map[string]context.CancelFunc{}
	}
	if s.client == nil {
		s.client = httpegress.Client(5 * time.Second)
	}
	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/jobs", s.wrap(principalFrom, s.listJobs))
	mux.HandleFunc("POST /v1/projects/{project}/locations/{location}/jobs", s.wrap(principalFrom, s.createJob))
	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/jobs/{job}", s.wrap(principalFrom, s.getJob))
	mux.HandleFunc("PATCH /v1/projects/{project}/locations/{location}/jobs/{job}", s.wrap(principalFrom, s.patchJob))
	mux.HandleFunc("DELETE /v1/projects/{project}/locations/{location}/jobs/{job}", s.wrap(principalFrom, s.deleteJob))
	mux.HandleFunc("POST /v1/projects/{project}/locations/{location}/jobs/{job}", s.wrap(principalFrom, s.jobAction))
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

func jobName(project, location, id string) string {
	return fmt.Sprintf("projects/%s/locations/%s/jobs/%s", project, location, id)
}

func (s *Service) createJob(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	if err := s.require(p, "cloudscheduler.jobs.create", project); err != nil {
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
			if len(parts) > 0 {
				jobID = parts[len(parts)-1]
			}
		}
	}
	if jobID == "" {
		gcperrors.InvalidArgument(w, "jobId is required")
		return
	}
	job := parseJobBody(project, location, jobID, body)
	if job.HTTPTargetJSON != "" {
		var ht struct {
			URI string `json:"uri"`
		}
		if err := json.Unmarshal([]byte(job.HTTPTargetJSON), &ht); err == nil && strings.TrimSpace(ht.URI) != "" {
			if err := httpegress.Validate(ht.URI); err != nil {
				gcperrors.InvalidArgument(w, err.Error())
				return
			}
		}
	}
	created, err := s.Store.CreateSchedulerJob(job)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "job already exists")
		return
	}
	s.registerTicker(job.Name)
	out, ok, err := s.Store.GetSchedulerJob(job.Name)
	if err != nil || !ok {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "created job missing")
		return
	}
	writeJSON(w, http.StatusOK, toJobJSON(out))
}

func (s *Service) getJob(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	id, _ := splitAction(r.PathValue("job"))
	if err := s.require(p, "cloudscheduler.jobs.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := jobName(project, location, id)
	job, ok, err := s.Store.GetSchedulerJob(name)
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
	if err := s.require(p, "cloudscheduler.jobs.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	list, err := s.Store.ListSchedulerJobs(project, location)
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
	if err := s.require(p, "cloudscheduler.jobs.update", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := jobName(project, location, id)
	existing, ok, err := s.Store.GetSchedulerJob(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Job not found")
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body == nil {
		body = map[string]any{}
	}
	updated := parseJobBody(project, location, id, body)
	if updated.Schedule == "" {
		updated.Schedule = existing.Schedule
	}
	if updated.TimeZone == "" {
		updated.TimeZone = existing.TimeZone
	}
	if updated.State == "" {
		updated.State = existing.State
	}
	if updated.HTTPTargetJSON == "" {
		updated.HTTPTargetJSON = existing.HTTPTargetJSON
		updated.OIDCAudience = existing.OIDCAudience
	}
	if updated.PubsubTargetJSON == "" {
		updated.PubsubTargetJSON = existing.PubsubTargetJSON
	}
	if updated.OIDCAudience == "" {
		updated.OIDCAudience = existing.OIDCAudience
	}
	updated.LastAttemptTime = existing.LastAttemptTime
	updated.CreatedAt = existing.CreatedAt
	updated.NextRunTime = store.NextCronRunRFC3339(updated.Schedule, updated.TimeZone, time.Now().UTC())
	if updated.HTTPTargetJSON != "" {
		var ht struct {
			URI string `json:"uri"`
		}
		if err := json.Unmarshal([]byte(updated.HTTPTargetJSON), &ht); err == nil && strings.TrimSpace(ht.URI) != "" {
			if err := httpegress.Validate(ht.URI); err != nil {
				gcperrors.InvalidArgument(w, err.Error())
				return
			}
		}
	}
	if _, err := s.Store.UpdateSchedulerJob(updated); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	s.registerTicker(name)
	out, _, _ := s.Store.GetSchedulerJob(name)
	writeJSON(w, http.StatusOK, toJobJSON(out))
}

func (s *Service) deleteJob(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	id, _ := splitAction(r.PathValue("job"))
	if err := s.require(p, "cloudscheduler.jobs.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := jobName(project, location, id)
	s.stopTicker(name)
	ok, err := s.Store.DeleteSchedulerJob(name)
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

func (s *Service) jobAction(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	id, action := splitAction(r.PathValue("job"))
	switch action {
	case "run":
		s.runJob(w, r, p, project, location, id)
	case "pause":
		s.setJobState(w, r, p, project, location, id, "PAUSED")
	case "resume":
		s.setJobState(w, r, p, project, location, id, "ENABLED")
	default:
		gcperrors.NotFound(w, "unknown Scheduler method")
	}
}

func (s *Service) setJobState(w http.ResponseWriter, _ *http.Request, p authn.Principal, project, location, id, state string) {
	if err := s.require(p, "cloudscheduler.jobs.update", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := jobName(project, location, id)
	job, ok, err := s.Store.GetSchedulerJob(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Job not found")
		return
	}
	job.State = state
	if _, err := s.Store.UpdateSchedulerJob(job); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if state == "ENABLED" {
		s.registerTicker(name)
	} else {
		s.stopTicker(name)
	}
	out, _, _ := s.Store.GetSchedulerJob(name)
	writeJSON(w, http.StatusOK, toJobJSON(out))
}

func (s *Service) runJob(w http.ResponseWriter, _ *http.Request, p authn.Principal, project, location, id string) {
	if err := s.require(p, "cloudscheduler.jobs.run", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := jobName(project, location, id)
	job, ok, err := s.Store.GetSchedulerJob(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Job not found")
		return
	}
	s.fire(job)
	out, _, _ := s.Store.GetSchedulerJob(name)
	writeJSON(w, http.StatusOK, toJobJSON(out))
}

func parseJobBody(project, location, jobID string, body map[string]any) store.SchedulerJob {
	schedule, _ := body["schedule"].(string)
	tz, _ := body["timeZone"].(string)
	state, _ := body["state"].(string)
	httpJSON := ""
	pubsubJSON := ""
	oidcAud := ""
	if ht, ok := body["httpTarget"].(map[string]any); ok {
		raw, _ := json.Marshal(ht)
		httpJSON, oidcAud = store.StripSchedulerOIDC(string(raw))
	}
	if pt, ok := body["pubsubTarget"].(map[string]any); ok {
		raw, _ := json.Marshal(pt)
		pubsubJSON = string(raw)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return store.SchedulerJob{
		Name:             jobName(project, location, jobID),
		ProjectID:        project,
		Location:         location,
		JobID:            jobID,
		Schedule:         schedule,
		TimeZone:         tz,
		State:            state,
		HTTPTargetJSON:   httpJSON,
		PubsubTargetJSON: pubsubJSON,
		OIDCAudience:     oidcAud,
		NextRunTime:      store.NextCronRunRFC3339(schedule, tz, time.Now().UTC()),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
}

func toJobJSON(job store.SchedulerJob) map[string]any {
	out := map[string]any{
		"name":            job.Name,
		"schedule":        job.Schedule,
		"timeZone":        job.TimeZone,
		"state":           job.State,
		"userUpdateTime":  job.UpdatedAt,
		"lastAttemptTime": job.LastAttemptTime,
		"scheduleTime":    job.NextRunTime,
	}
	if job.NextRunTime != "" {
		out["scheduleTime"] = job.NextRunTime
	}
	if job.HTTPTargetJSON != "" {
		var ht any
		_ = json.Unmarshal([]byte(job.HTTPTargetJSON), &ht)
		out["httpTarget"] = ht
	}
	if job.OIDCAudience != "" {
		out["oidcTokenAudience"] = job.OIDCAudience
	}
	if job.PubsubTargetJSON != "" {
		var pt any
		_ = json.Unmarshal([]byte(job.PubsubTargetJSON), &pt)
		out["pubsubTarget"] = pt
	}
	return out
}

func (s *Service) registerTicker(name string) {
	s.stopTicker(name)
	job, ok, err := s.Store.GetSchedulerJob(name)
	if err != nil || !ok || job.State != "ENABLED" {
		return
	}
	interval := tickerInterval(job.Schedule)
	if interval <= 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.tickers[name] = cancel
	s.mu.Unlock()
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				j, ok, err := s.Store.GetSchedulerJob(name)
				if err != nil || !ok || j.State != "ENABLED" {
					return
				}
				s.fire(j)
			}
		}
	}()
}

func (s *Service) stopTicker(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancel, ok := s.tickers[name]; ok {
		cancel()
		delete(s.tickers, name)
	}
}

// tickerInterval returns a positive duration for every-minute cron theatre, else 0 (schedule-only / :run).
func tickerInterval(schedule string) time.Duration {
	schedule = strings.TrimSpace(schedule)
	switch schedule {
	case "* * * * *", "*/1 * * * *":
		return time.Minute
	}
	if strings.HasPrefix(schedule, "*/") {
		rest := strings.TrimPrefix(schedule, "*/")
		fields := strings.Fields(rest)
		if len(fields) >= 1 {
			var n int
			if _, err := fmt.Sscanf(fields[0], "%d", &n); err == nil && n > 0 && n <= 60 {
				return time.Duration(n) * time.Minute
			}
		}
	}
	return 0
}

func (s *Service) fire(job store.SchedulerJob) {
	_ = s.Store.MarkSchedulerJobAttempt(job.Name)
	if job.HTTPTargetJSON != "" {
		var ht struct {
			URI        string            `json:"uri"`
			HttpMethod string            `json:"httpMethod"`
			Headers    map[string]string `json:"headers"`
			Body       string            `json:"body"`
		}
		if err := json.Unmarshal([]byte(job.HTTPTargetJSON), &ht); err == nil && ht.URI != "" {
			if err := httpegress.Validate(ht.URI); err != nil {
				return
			}
			method := ht.HttpMethod
			if method == "" {
				method = http.MethodPost
			}
			var body []byte
			if ht.Body != "" {
				if decoded, err := base64.StdEncoding.DecodeString(ht.Body); err == nil {
					body = decoded
				} else {
					body = []byte(ht.Body)
				}
			}
			req, err := http.NewRequest(method, ht.URI, bytes.NewReader(body))
			if err == nil {
				for k, v := range ht.Headers {
					req.Header.Set(k, v)
				}
				resp, err := s.client.Do(req)
				if err == nil {
					_ = resp.Body.Close()
				}
			}
		}
	}
	if job.PubsubTargetJSON != "" {
		var pt struct {
			TopicName string `json:"topicName"`
			Data      string `json:"data"`
		}
		if err := json.Unmarshal([]byte(job.PubsubTargetJSON), &pt); err == nil && pt.TopicName != "" {
			var data []byte
			if pt.Data != "" {
				if decoded, err := base64.StdEncoding.DecodeString(pt.Data); err == nil {
					data = decoded
				} else {
					data = []byte(pt.Data)
				}
			}
			_, _ = s.Store.Publish(pt.TopicName, data, nil)
		}
	}
}
