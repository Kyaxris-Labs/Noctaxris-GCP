package dataflow

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// DefaultLocation is the lab default Dataflow regional endpoint.
const DefaultLocation = "us-central1"

// Service serves Cloud Dataflow REST v1b3 (jobs create/get/list theatre; no workers).
type Service struct {
	Store *store.Store
	Authz *authz.Evaluator
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// Mount registers Dataflow v1b3 REST routes.
// Colon methods on job segments are parsed via splitAction (none required for this cut).
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	mux.HandleFunc("POST /v1b3/projects/{project}/locations/{location}/jobs", s.wrap(principalFrom, s.createJob))
	mux.HandleFunc("GET /v1b3/projects/{project}/locations/{location}/jobs", s.wrap(principalFrom, s.listJobsRegional))
	mux.HandleFunc("GET /v1b3/projects/{project}/locations/{location}/jobs/{job}", s.wrap(principalFrom, s.getJob))

	mux.HandleFunc("GET /v1b3/projects/{project}/jobs", s.wrap(principalFrom, s.listJobsProject))
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

func (s *Service) createJob(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	if err := s.require(p, "dataflow.jobs.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body == nil {
		body = map[string]any{}
	}
	jobName, _ := body["name"].(string)
	jobType, _ := body["type"].(string)
	if jobType == "" {
		jobType = "JOB_TYPE_BATCH"
	}
	switch jobType {
	case "JOB_TYPE_BATCH", "JOB_TYPE_STREAMING", "JOB_TYPE_UNKNOWN":
	default:
		// Accept custom strings for lab flexibility, but document batch theatre only.
	}
	jobID := store.NewDataflowJobID()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	name := store.DataflowJobResourceName(project, location, jobID)

	// Preserve client fields in job_json; override server-owned state fields on read.
	body["id"] = jobID
	body["projectId"] = project
	body["location"] = location
	if jobName != "" {
		body["name"] = jobName
	}
	body["type"] = jobType
	body["currentState"] = "JOB_STATE_RUNNING"
	body["currentStateTime"] = now
	body["createTime"] = now
	body["startTime"] = now
	raw, _ := json.Marshal(body)

	created, err := s.Store.CreateDataflowJob(store.DataflowJob{
		Name: name, ProjectID: project, Location: location, JobID: jobID,
		JobName: jobName, JobType: jobType, CurrentState: "JOB_STATE_RUNNING",
		JobJSON: string(raw), CreatedAt: now, CurrentStateTime: now, StartTime: now,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "job already exists")
		return
	}
	out, _, _ := s.Store.GetDataflowJob(name)
	writeJSON(w, http.StatusOK, toJobJSON(out))
}

func (s *Service) getJob(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	jobID, action := splitAction(r.PathValue("job"))
	if action != "" {
		gcperrors.NotFound(w, "unknown Dataflow method")
		return
	}
	if err := s.require(p, "dataflow.jobs.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := store.DataflowJobResourceName(project, location, jobID)
	// Theatre: create returns JOB_STATE_RUNNING; get advances RUNNING → JOB_STATE_DONE.
	j, ok, err := s.Store.AdvanceDataflowJobToDone(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Job not found")
		return
	}
	writeJSON(w, http.StatusOK, toJobJSON(j))
}

func (s *Service) listJobsRegional(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	if err := s.require(p, "dataflow.jobs.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	list, err := s.Store.ListDataflowJobs(project, location)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobsJSON(list)})
}

func (s *Service) listJobsProject(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "dataflow.jobs.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	list, err := s.Store.ListDataflowJobsProject(project)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobsJSON(list)})
}

func jobsJSON(list []store.DataflowJob) []map[string]any {
	items := make([]map[string]any, 0, len(list))
	for _, j := range list {
		items = append(items, toJobJSON(j))
	}
	return items
}

func toJobJSON(j store.DataflowJob) map[string]any {
	out := map[string]any{}
	_ = json.Unmarshal([]byte(j.JobJSON), &out)
	if out == nil {
		out = map[string]any{}
	}
	out["id"] = j.JobID
	out["projectId"] = j.ProjectID
	out["location"] = j.Location
	out["type"] = j.JobType
	out["currentState"] = j.CurrentState
	out["currentStateTime"] = j.CurrentStateTime
	out["createTime"] = j.CreatedAt
	out["startTime"] = j.StartTime
	if j.JobName != "" {
		out["name"] = j.JobName
	}
	return out
}
