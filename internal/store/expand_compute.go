package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// RunService is a Cloud Run v2 service row (lab subset).
type RunService struct {
	Name            string
	ProjectID       string
	Location        string
	ServiceID       string
	UID             string
	Generation      int64
	TemplateJSON    string
	URI             string
	LatestRevision  string
	LabResponseBody string
	LastInvokeJSON  string
	CreatedAt       string
	UpdatedAt       string
}

// RunRevision is a Cloud Run revision metadata row.
type RunRevision struct {
	Name         string
	ServiceName  string
	Generation   int64
	TemplateJSON string
	CreatedAt    string
}

// CloudFunction is a Cloud Functions v2 function row.
type CloudFunction struct {
	Name            string
	ProjectID       string
	Location        string
	FunctionID      string
	State           string
	ConfigJSON      string
	URI             string
	LabResponseJSON string
	CreatedAt       string
	UpdatedAt       string
}

// SchedulerJob is a Cloud Scheduler job row.
type SchedulerJob struct {
	Name             string
	ProjectID        string
	Location         string
	JobID            string
	Schedule         string
	TimeZone         string
	State            string
	HTTPTargetJSON   string
	PubsubTargetJSON string
	LastAttemptTime  string
	CreatedAt        string
	UpdatedAt        string
}

// CloudTasksQueue is a Cloud Tasks queue row.
type CloudTasksQueue struct {
	Name      string
	ProjectID string
	Location  string
	QueueID   string
	State     string
	CreatedAt string
}

// CloudTask is a Cloud Tasks task row (OIDC/OAuth tokens stripped from http_request_json).
type CloudTask struct {
	Name            string
	QueueName       string
	ScheduleTime    string
	CreateTime      string
	HTTPRequestJSON string
	DispatchCount   int
}

// CreateRunService inserts a service and its first revision.
func (s *Store) CreateRunService(svc RunService) (created bool, err error) {
	if svc.Name == "" || svc.ProjectID == "" || svc.Location == "" || svc.ServiceID == "" {
		return false, fmt.Errorf("run service requires name, project, location, and service id")
	}
	if svc.UID == "" {
		svc.UID = uuid.NewString()
	}
	if svc.Generation == 0 {
		svc.Generation = 1
	}
	if svc.TemplateJSON == "" {
		svc.TemplateJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if svc.CreatedAt == "" {
		svc.CreatedAt = now
	}
	if svc.UpdatedAt == "" {
		svc.UpdatedAt = svc.CreatedAt
	}
	revName := fmt.Sprintf("%s/revisions/%s-%05d", svc.Name, svc.ServiceID, svc.Generation)
	if svc.LatestRevision == "" {
		svc.LatestRevision = revName
	}
	if svc.URI == "" {
		svc.URI = fmt.Sprintf("http://127.0.0.1:4588/v2/%s:invoke", svc.Name)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.Exec(
		`INSERT OR IGNORE INTO run_services
		 (name, project_id, location, service_id, uid, generation, template_json, uri, latest_revision, lab_response_body, last_invoke_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		svc.Name, svc.ProjectID, svc.Location, svc.ServiceID, svc.UID, svc.Generation,
		svc.TemplateJSON, svc.URI, svc.LatestRevision, svc.LabResponseBody, svc.LastInvokeJSON,
		svc.CreatedAt, svc.UpdatedAt,
	)
	if err != nil {
		return false, fmt.Errorf("create run service: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 0 {
		return false, nil
	}
	if _, err := tx.Exec(
		`INSERT INTO run_revisions (name, service_name, generation, template_json, created_at) VALUES (?, ?, ?, ?, ?)`,
		revName, svc.Name, svc.Generation, svc.TemplateJSON, svc.CreatedAt,
	); err != nil {
		return false, fmt.Errorf("create run revision: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// GetRunService loads a service by name.
func (s *Store) GetRunService(name string) (RunService, bool, error) {
	var svc RunService
	err := s.db.QueryRow(
		`SELECT name, project_id, location, service_id, uid, generation, template_json, uri, latest_revision,
		        lab_response_body, last_invoke_json, created_at, updated_at
		 FROM run_services WHERE name = ?`, name,
	).Scan(
		&svc.Name, &svc.ProjectID, &svc.Location, &svc.ServiceID, &svc.UID, &svc.Generation,
		&svc.TemplateJSON, &svc.URI, &svc.LatestRevision, &svc.LabResponseBody, &svc.LastInvokeJSON,
		&svc.CreatedAt, &svc.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return RunService{}, false, nil
	}
	if err != nil {
		return RunService{}, false, fmt.Errorf("get run service: %w", err)
	}
	return svc, true, nil
}

// ListRunServices lists services under project/location.
func (s *Store) ListRunServices(projectID, location string) ([]RunService, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, location, service_id, uid, generation, template_json, uri, latest_revision,
		        lab_response_body, last_invoke_json, created_at, updated_at
		 FROM run_services WHERE project_id = ? AND location = ? ORDER BY name`,
		projectID, location,
	)
	if err != nil {
		return nil, fmt.Errorf("list run services: %w", err)
	}
	defer rows.Close()
	var out []RunService
	for rows.Next() {
		var svc RunService
		if err := rows.Scan(
			&svc.Name, &svc.ProjectID, &svc.Location, &svc.ServiceID, &svc.UID, &svc.Generation,
			&svc.TemplateJSON, &svc.URI, &svc.LatestRevision, &svc.LabResponseBody, &svc.LastInvokeJSON,
			&svc.CreatedAt, &svc.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, svc)
	}
	return out, rows.Err()
}

// UpdateRunService patches template/lab body and bumps generation with a new revision.
func (s *Store) UpdateRunService(name, templateJSON, labResponseBody string) (RunService, bool, error) {
	svc, ok, err := s.GetRunService(name)
	if err != nil || !ok {
		return RunService{}, ok, err
	}
	if templateJSON != "" {
		svc.TemplateJSON = templateJSON
	}
	if labResponseBody != "" {
		svc.LabResponseBody = labResponseBody
	}
	svc.Generation++
	svc.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	revName := fmt.Sprintf("%s/revisions/%s-%05d", svc.Name, svc.ServiceID, svc.Generation)
	svc.LatestRevision = revName

	tx, err := s.db.Begin()
	if err != nil {
		return RunService{}, false, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(
		`UPDATE run_services SET generation = ?, template_json = ?, latest_revision = ?, lab_response_body = ?, updated_at = ?
		 WHERE name = ?`,
		svc.Generation, svc.TemplateJSON, svc.LatestRevision, svc.LabResponseBody, svc.UpdatedAt, name,
	); err != nil {
		return RunService{}, false, fmt.Errorf("update run service: %w", err)
	}
	if _, err := tx.Exec(
		`INSERT INTO run_revisions (name, service_name, generation, template_json, created_at) VALUES (?, ?, ?, ?, ?)`,
		revName, svc.Name, svc.Generation, svc.TemplateJSON, svc.UpdatedAt,
	); err != nil {
		return RunService{}, false, fmt.Errorf("insert run revision: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return RunService{}, false, err
	}
	return svc, true, nil
}

// DeleteRunService removes a service and its revisions.
func (s *Store) DeleteRunService(name string) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM run_revisions WHERE service_name = ?`, name); err != nil {
		return false, err
	}
	res, err := tx.Exec(`DELETE FROM run_services WHERE name = ?`, name)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return n > 0, nil
}

// ListRunRevisions lists revisions for a service, newest first.
func (s *Store) ListRunRevisions(serviceName string) ([]RunRevision, error) {
	rows, err := s.db.Query(
		`SELECT name, service_name, generation, template_json, created_at
		 FROM run_revisions WHERE service_name = ? ORDER BY generation DESC`,
		serviceName,
	)
	if err != nil {
		return nil, fmt.Errorf("list run revisions: %w", err)
	}
	defer rows.Close()
	var out []RunRevision
	for rows.Next() {
		var r RunRevision
		if err := rows.Scan(&r.Name, &r.ServiceName, &r.Generation, &r.TemplateJSON, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RecordRunInvoke stores the last invoke payload for a service.
func (s *Store) RecordRunInvoke(name, invokeJSON string) error {
	_, err := s.db.Exec(`UPDATE run_services SET last_invoke_json = ? WHERE name = ?`, invokeJSON, name)
	if err != nil {
		return fmt.Errorf("record run invoke: %w", err)
	}
	return nil
}

// CreateCloudFunction inserts a function. created=false means already exists.
func (s *Store) CreateCloudFunction(fn CloudFunction) (created bool, err error) {
	if fn.Name == "" || fn.ProjectID == "" || fn.Location == "" || fn.FunctionID == "" {
		return false, fmt.Errorf("cloud function requires name, project, location, and function id")
	}
	if fn.State == "" {
		fn.State = "ACTIVE"
	}
	if fn.ConfigJSON == "" {
		fn.ConfigJSON = "{}"
	}
	if fn.LabResponseJSON == "" {
		fn.LabResponseJSON = `{"ok":true}`
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if fn.CreatedAt == "" {
		fn.CreatedAt = now
	}
	if fn.UpdatedAt == "" {
		fn.UpdatedAt = fn.CreatedAt
	}
	if fn.URI == "" {
		fn.URI = fmt.Sprintf("http://127.0.0.1:4588/v2/%s:invoke", fn.Name)
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO cloud_functions
		 (name, project_id, location, function_id, state, config_json, uri, lab_response_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		fn.Name, fn.ProjectID, fn.Location, fn.FunctionID, fn.State, fn.ConfigJSON, fn.URI,
		fn.LabResponseJSON, fn.CreatedAt, fn.UpdatedAt,
	)
	if err != nil {
		return false, fmt.Errorf("create cloud function: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetCloudFunction loads a function by name.
func (s *Store) GetCloudFunction(name string) (CloudFunction, bool, error) {
	var fn CloudFunction
	err := s.db.QueryRow(
		`SELECT name, project_id, location, function_id, state, config_json, uri, lab_response_json, created_at, updated_at
		 FROM cloud_functions WHERE name = ?`, name,
	).Scan(
		&fn.Name, &fn.ProjectID, &fn.Location, &fn.FunctionID, &fn.State, &fn.ConfigJSON,
		&fn.URI, &fn.LabResponseJSON, &fn.CreatedAt, &fn.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return CloudFunction{}, false, nil
	}
	if err != nil {
		return CloudFunction{}, false, fmt.Errorf("get cloud function: %w", err)
	}
	return fn, true, nil
}

// ListCloudFunctions lists functions under project/location.
func (s *Store) ListCloudFunctions(projectID, location string) ([]CloudFunction, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, location, function_id, state, config_json, uri, lab_response_json, created_at, updated_at
		 FROM cloud_functions WHERE project_id = ? AND location = ? ORDER BY name`,
		projectID, location,
	)
	if err != nil {
		return nil, fmt.Errorf("list cloud functions: %w", err)
	}
	defer rows.Close()
	var out []CloudFunction
	for rows.Next() {
		var fn CloudFunction
		if err := rows.Scan(
			&fn.Name, &fn.ProjectID, &fn.Location, &fn.FunctionID, &fn.State, &fn.ConfigJSON,
			&fn.URI, &fn.LabResponseJSON, &fn.CreatedAt, &fn.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, fn)
	}
	return out, rows.Err()
}

// UpdateCloudFunction patches config and/or lab response.
func (s *Store) UpdateCloudFunction(name, configJSON, labResponseJSON string) (CloudFunction, bool, error) {
	fn, ok, err := s.GetCloudFunction(name)
	if err != nil || !ok {
		return CloudFunction{}, ok, err
	}
	if configJSON != "" {
		fn.ConfigJSON = configJSON
	}
	if labResponseJSON != "" {
		fn.LabResponseJSON = labResponseJSON
	}
	fn.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.Exec(
		`UPDATE cloud_functions SET config_json = ?, lab_response_json = ?, updated_at = ? WHERE name = ?`,
		fn.ConfigJSON, fn.LabResponseJSON, fn.UpdatedAt, name,
	)
	if err != nil {
		return CloudFunction{}, false, fmt.Errorf("update cloud function: %w", err)
	}
	return fn, true, nil
}

// DeleteCloudFunction removes a function.
func (s *Store) DeleteCloudFunction(name string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM cloud_functions WHERE name = ?`, name)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// CreateSchedulerJob inserts a job. created=false means already exists.
func (s *Store) CreateSchedulerJob(job SchedulerJob) (created bool, err error) {
	if job.Name == "" || job.ProjectID == "" || job.Location == "" || job.JobID == "" {
		return false, fmt.Errorf("scheduler job requires name, project, location, and job id")
	}
	if job.State == "" {
		job.State = "ENABLED"
	}
	if job.TimeZone == "" {
		job.TimeZone = "UTC"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if job.CreatedAt == "" {
		job.CreatedAt = now
	}
	if job.UpdatedAt == "" {
		job.UpdatedAt = job.CreatedAt
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO scheduler_jobs
		 (name, project_id, location, job_id, schedule, time_zone, state, http_target_json, pubsub_target_json, last_attempt_time, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.Name, job.ProjectID, job.Location, job.JobID, job.Schedule, job.TimeZone, job.State,
		job.HTTPTargetJSON, job.PubsubTargetJSON, job.LastAttemptTime, job.CreatedAt, job.UpdatedAt,
	)
	if err != nil {
		return false, fmt.Errorf("create scheduler job: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetSchedulerJob loads a job by name.
func (s *Store) GetSchedulerJob(name string) (SchedulerJob, bool, error) {
	var job SchedulerJob
	err := s.db.QueryRow(
		`SELECT name, project_id, location, job_id, schedule, time_zone, state, http_target_json, pubsub_target_json,
		        last_attempt_time, created_at, updated_at
		 FROM scheduler_jobs WHERE name = ?`, name,
	).Scan(
		&job.Name, &job.ProjectID, &job.Location, &job.JobID, &job.Schedule, &job.TimeZone, &job.State,
		&job.HTTPTargetJSON, &job.PubsubTargetJSON, &job.LastAttemptTime, &job.CreatedAt, &job.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return SchedulerJob{}, false, nil
	}
	if err != nil {
		return SchedulerJob{}, false, fmt.Errorf("get scheduler job: %w", err)
	}
	return job, true, nil
}

// ListSchedulerJobs lists jobs under project/location.
func (s *Store) ListSchedulerJobs(projectID, location string) ([]SchedulerJob, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, location, job_id, schedule, time_zone, state, http_target_json, pubsub_target_json,
		        last_attempt_time, created_at, updated_at
		 FROM scheduler_jobs WHERE project_id = ? AND location = ? ORDER BY name`,
		projectID, location,
	)
	if err != nil {
		return nil, fmt.Errorf("list scheduler jobs: %w", err)
	}
	defer rows.Close()
	var out []SchedulerJob
	for rows.Next() {
		var job SchedulerJob
		if err := rows.Scan(
			&job.Name, &job.ProjectID, &job.Location, &job.JobID, &job.Schedule, &job.TimeZone, &job.State,
			&job.HTTPTargetJSON, &job.PubsubTargetJSON, &job.LastAttemptTime, &job.CreatedAt, &job.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

// UpdateSchedulerJob replaces mutable job fields.
func (s *Store) UpdateSchedulerJob(job SchedulerJob) (bool, error) {
	job.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.Exec(
		`UPDATE scheduler_jobs SET schedule = ?, time_zone = ?, state = ?, http_target_json = ?, pubsub_target_json = ?,
		 last_attempt_time = ?, updated_at = ? WHERE name = ?`,
		job.Schedule, job.TimeZone, job.State, job.HTTPTargetJSON, job.PubsubTargetJSON,
		job.LastAttemptTime, job.UpdatedAt, job.Name,
	)
	if err != nil {
		return false, fmt.Errorf("update scheduler job: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// DeleteSchedulerJob removes a job.
func (s *Store) DeleteSchedulerJob(name string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM scheduler_jobs WHERE name = ?`, name)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// MarkSchedulerJobAttempt sets last_attempt_time.
func (s *Store) MarkSchedulerJobAttempt(name string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.Exec(
		`UPDATE scheduler_jobs SET last_attempt_time = ?, updated_at = ? WHERE name = ?`,
		now, now, name,
	)
	if err != nil {
		return fmt.Errorf("mark scheduler attempt: %w", err)
	}
	return nil
}

// CreateCloudTasksQueue inserts a queue. created=false means already exists.
func (s *Store) CreateCloudTasksQueue(q CloudTasksQueue) (created bool, err error) {
	if q.Name == "" || q.ProjectID == "" || q.Location == "" || q.QueueID == "" {
		return false, fmt.Errorf("cloud tasks queue requires name, project, location, and queue id")
	}
	if q.State == "" {
		q.State = "RUNNING"
	}
	if q.CreatedAt == "" {
		q.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO cloud_tasks_queues (name, project_id, location, queue_id, state, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		q.Name, q.ProjectID, q.Location, q.QueueID, q.State, q.CreatedAt,
	)
	if err != nil {
		return false, fmt.Errorf("create cloud tasks queue: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetCloudTasksQueue loads a queue by name.
func (s *Store) GetCloudTasksQueue(name string) (CloudTasksQueue, bool, error) {
	var q CloudTasksQueue
	err := s.db.QueryRow(
		`SELECT name, project_id, location, queue_id, state, created_at FROM cloud_tasks_queues WHERE name = ?`, name,
	).Scan(&q.Name, &q.ProjectID, &q.Location, &q.QueueID, &q.State, &q.CreatedAt)
	if err == sql.ErrNoRows {
		return CloudTasksQueue{}, false, nil
	}
	if err != nil {
		return CloudTasksQueue{}, false, fmt.Errorf("get cloud tasks queue: %w", err)
	}
	return q, true, nil
}

// ListCloudTasksQueues lists queues under project/location.
func (s *Store) ListCloudTasksQueues(projectID, location string) ([]CloudTasksQueue, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, location, queue_id, state, created_at
		 FROM cloud_tasks_queues WHERE project_id = ? AND location = ? ORDER BY name`,
		projectID, location,
	)
	if err != nil {
		return nil, fmt.Errorf("list cloud tasks queues: %w", err)
	}
	defer rows.Close()
	var out []CloudTasksQueue
	for rows.Next() {
		var q CloudTasksQueue
		if err := rows.Scan(&q.Name, &q.ProjectID, &q.Location, &q.QueueID, &q.State, &q.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// DeleteCloudTasksQueue removes a queue and its tasks.
func (s *Store) DeleteCloudTasksQueue(name string) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM cloud_tasks WHERE queue_name = ?`, name); err != nil {
		return false, err
	}
	res, err := tx.Exec(`DELETE FROM cloud_tasks_queues WHERE name = ?`, name)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return n > 0, nil
}

// StripTaskAuthTokens removes oidcToken/oauthToken from an httpRequest JSON object.
func StripTaskAuthTokens(httpRequestJSON string) string {
	if strings.TrimSpace(httpRequestJSON) == "" {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(httpRequestJSON), &m); err != nil {
		return httpRequestJSON
	}
	delete(m, "oidcToken")
	delete(m, "oauthToken")
	raw, err := json.Marshal(m)
	if err != nil {
		return httpRequestJSON
	}
	return string(raw)
}

// CreateCloudTask inserts a task. created=false means already exists.
func (s *Store) CreateCloudTask(task CloudTask) (created bool, err error) {
	if task.Name == "" || task.QueueName == "" {
		return false, fmt.Errorf("cloud task requires name and queue")
	}
	if task.CreateTime == "" {
		task.CreateTime = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if task.ScheduleTime == "" {
		task.ScheduleTime = task.CreateTime
	}
	task.HTTPRequestJSON = StripTaskAuthTokens(task.HTTPRequestJSON)
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO cloud_tasks (name, queue_name, schedule_time, create_time, http_request_json, dispatch_count)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		task.Name, task.QueueName, task.ScheduleTime, task.CreateTime, task.HTTPRequestJSON, task.DispatchCount,
	)
	if err != nil {
		return false, fmt.Errorf("create cloud task: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetCloudTask loads a task by name.
func (s *Store) GetCloudTask(name string) (CloudTask, bool, error) {
	var t CloudTask
	err := s.db.QueryRow(
		`SELECT name, queue_name, schedule_time, create_time, http_request_json, dispatch_count
		 FROM cloud_tasks WHERE name = ?`, name,
	).Scan(&t.Name, &t.QueueName, &t.ScheduleTime, &t.CreateTime, &t.HTTPRequestJSON, &t.DispatchCount)
	if err == sql.ErrNoRows {
		return CloudTask{}, false, nil
	}
	if err != nil {
		return CloudTask{}, false, fmt.Errorf("get cloud task: %w", err)
	}
	return t, true, nil
}

// ListCloudTasks lists tasks in a queue.
func (s *Store) ListCloudTasks(queueName string) ([]CloudTask, error) {
	rows, err := s.db.Query(
		`SELECT name, queue_name, schedule_time, create_time, http_request_json, dispatch_count
		 FROM cloud_tasks WHERE queue_name = ? ORDER BY create_time`,
		queueName,
	)
	if err != nil {
		return nil, fmt.Errorf("list cloud tasks: %w", err)
	}
	defer rows.Close()
	var out []CloudTask
	for rows.Next() {
		var t CloudTask
		if err := rows.Scan(&t.Name, &t.QueueName, &t.ScheduleTime, &t.CreateTime, &t.HTTPRequestJSON, &t.DispatchCount); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteCloudTask removes a task.
func (s *Store) DeleteCloudTask(name string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM cloud_tasks WHERE name = ?`, name)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// IncrementCloudTaskDispatch bumps dispatch_count and returns the updated task.
func (s *Store) IncrementCloudTaskDispatch(name string) (CloudTask, bool, error) {
	res, err := s.db.Exec(`UPDATE cloud_tasks SET dispatch_count = dispatch_count + 1 WHERE name = ?`, name)
	if err != nil {
		return CloudTask{}, false, fmt.Errorf("increment task dispatch: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return CloudTask{}, false, err
	}
	if n == 0 {
		return CloudTask{}, false, nil
	}
	return s.GetCloudTask(name)
}
