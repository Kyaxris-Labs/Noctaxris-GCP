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
	TrafficJSON     string
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

// RunJob is a Cloud Run v2 job row (control-plane theatre, no container start).
type RunJob struct {
	Name         string
	ProjectID    string
	Location     string
	JobID        string
	UID          string
	Generation   int64
	TemplateJSON string
	CreatedAt    string
	UpdatedAt    string
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
	OIDCAudience     string
	NextRunTime      string
	LastAttemptTime  string
	CreatedAt        string
	UpdatedAt        string
}

// CloudTasksQueue is a Cloud Tasks queue row.
type CloudTasksQueue struct {
	Name                         string
	ProjectID                    string
	Location                     string
	QueueID                      string
	State                        string
	RateLimitsJSON               string
	RetryConfigJSON              string
	AppEngineRoutingOverrideJSON string
	CreatedAt                    string
}

// CloudTask is a Cloud Tasks task row (OIDC/OAuth tokens stripped from http_request_json).
type CloudTask struct {
	Name                      string
	QueueName                 string
	ScheduleTime              string
	CreateTime                string
	HTTPRequestJSON           string
	AppEngineHTTPRequestJSON  string
	DispatchCount             int
	ResponseCount             int
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
	if svc.TrafficJSON == "" {
		svc.TrafficJSON = fmt.Sprintf(`[{"type":"TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST","percent":100,"revision":%q}]`, svc.LatestRevision)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.Exec(
		`INSERT OR IGNORE INTO run_services
		 (name, project_id, location, service_id, uid, generation, template_json, uri, latest_revision, lab_response_body, last_invoke_json, traffic_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		svc.Name, svc.ProjectID, svc.Location, svc.ServiceID, svc.UID, svc.Generation,
		svc.TemplateJSON, svc.URI, svc.LatestRevision, svc.LabResponseBody, svc.LastInvokeJSON, svc.TrafficJSON,
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
		        lab_response_body, last_invoke_json, traffic_json, created_at, updated_at
		 FROM run_services WHERE name = ?`, name,
	).Scan(
		&svc.Name, &svc.ProjectID, &svc.Location, &svc.ServiceID, &svc.UID, &svc.Generation,
		&svc.TemplateJSON, &svc.URI, &svc.LatestRevision, &svc.LabResponseBody, &svc.LastInvokeJSON,
		&svc.TrafficJSON, &svc.CreatedAt, &svc.UpdatedAt,
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
		        lab_response_body, last_invoke_json, traffic_json, created_at, updated_at
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
			&svc.TrafficJSON, &svc.CreatedAt, &svc.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, svc)
	}
	return out, rows.Err()
}

// UpdateRunService patches template/lab body/traffic and bumps generation with a new revision.
func (s *Store) UpdateRunService(name, templateJSON, labResponseBody, trafficJSON string) (RunService, bool, error) {
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
	if trafficJSON != "" {
		svc.TrafficJSON = trafficJSON
	} else {
		svc.TrafficJSON = fmt.Sprintf(`[{"type":"TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST","percent":100,"revision":%q}]`, revName)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return RunService{}, false, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(
		`UPDATE run_services SET generation = ?, template_json = ?, latest_revision = ?, lab_response_body = ?, traffic_json = ?, updated_at = ?
		 WHERE name = ?`,
		svc.Generation, svc.TemplateJSON, svc.LatestRevision, svc.LabResponseBody, svc.TrafficJSON, svc.UpdatedAt, name,
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

// SetRunServiceTraffic updates traffic allocation without bumping generation.
func (s *Store) SetRunServiceTraffic(name, trafficJSON string) (RunService, bool, error) {
	svc, ok, err := s.GetRunService(name)
	if err != nil || !ok {
		return RunService{}, ok, err
	}
	if trafficJSON == "" {
		trafficJSON = "[]"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.Exec(`UPDATE run_services SET traffic_json = ?, updated_at = ? WHERE name = ?`, trafficJSON, now, name)
	if err != nil {
		return RunService{}, false, fmt.Errorf("set run traffic: %w", err)
	}
	svc.TrafficJSON = trafficJSON
	svc.UpdatedAt = now
	return svc, true, nil
}

// CreateRunJob inserts a Cloud Run job. created=false means already exists.
func (s *Store) CreateRunJob(job RunJob) (created bool, err error) {
	if job.Name == "" || job.ProjectID == "" || job.Location == "" || job.JobID == "" {
		return false, fmt.Errorf("run job requires name, project, location, and job id")
	}
	if job.UID == "" {
		job.UID = uuid.NewString()
	}
	if job.Generation == 0 {
		job.Generation = 1
	}
	if job.TemplateJSON == "" {
		job.TemplateJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if job.CreatedAt == "" {
		job.CreatedAt = now
	}
	if job.UpdatedAt == "" {
		job.UpdatedAt = job.CreatedAt
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO run_jobs
		 (name, project_id, location, job_id, uid, generation, template_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.Name, job.ProjectID, job.Location, job.JobID, job.UID, job.Generation, job.TemplateJSON, job.CreatedAt, job.UpdatedAt,
	)
	if err != nil {
		return false, fmt.Errorf("create run job: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetRunJob loads a job by name.
func (s *Store) GetRunJob(name string) (RunJob, bool, error) {
	var job RunJob
	err := s.db.QueryRow(
		`SELECT name, project_id, location, job_id, uid, generation, template_json, created_at, updated_at
		 FROM run_jobs WHERE name = ?`, name,
	).Scan(&job.Name, &job.ProjectID, &job.Location, &job.JobID, &job.UID, &job.Generation, &job.TemplateJSON, &job.CreatedAt, &job.UpdatedAt)
	if err == sql.ErrNoRows {
		return RunJob{}, false, nil
	}
	if err != nil {
		return RunJob{}, false, fmt.Errorf("get run job: %w", err)
	}
	return job, true, nil
}

// ListRunJobs lists jobs under project/location.
func (s *Store) ListRunJobs(projectID, location string) ([]RunJob, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, location, job_id, uid, generation, template_json, created_at, updated_at
		 FROM run_jobs WHERE project_id = ? AND location = ? ORDER BY name`,
		projectID, location,
	)
	if err != nil {
		return nil, fmt.Errorf("list run jobs: %w", err)
	}
	defer rows.Close()
	var out []RunJob
	for rows.Next() {
		var job RunJob
		if err := rows.Scan(&job.Name, &job.ProjectID, &job.Location, &job.JobID, &job.UID, &job.Generation, &job.TemplateJSON, &job.CreatedAt, &job.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

// UpdateRunJob patches template and bumps generation.
func (s *Store) UpdateRunJob(name, templateJSON string) (RunJob, bool, error) {
	job, ok, err := s.GetRunJob(name)
	if err != nil || !ok {
		return RunJob{}, ok, err
	}
	if templateJSON != "" {
		job.TemplateJSON = templateJSON
	}
	job.Generation++
	job.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.Exec(
		`UPDATE run_jobs SET generation = ?, template_json = ?, updated_at = ? WHERE name = ?`,
		job.Generation, job.TemplateJSON, job.UpdatedAt, name,
	)
	if err != nil {
		return RunJob{}, false, fmt.Errorf("update run job: %w", err)
	}
	return job, true, nil
}

// DeleteRunJob removes a job.
func (s *Store) DeleteRunJob(name string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM run_jobs WHERE name = ?`, name)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
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

// SetCloudFunctionState updates function state theatre (e.g. DEPLOYING → ACTIVE).
func (s *Store) SetCloudFunctionState(name, state string) (CloudFunction, bool, error) {
	if state == "" {
		return CloudFunction{}, false, fmt.Errorf("state required")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.Exec(
		`UPDATE cloud_functions SET state = ?, updated_at = ? WHERE name = ?`,
		state, now, name,
	)
	if err != nil {
		return CloudFunction{}, false, fmt.Errorf("set cloud function state: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return CloudFunction{}, false, err
	}
	if n == 0 {
		return CloudFunction{}, false, nil
	}
	return s.GetCloudFunction(name)
}

// CloudFunctionUpload is a lab source upload acceptance row.
type CloudFunctionUpload struct {
	UploadID   string
	ProjectID  string
	Location   string
	Bucket     string
	Object     string
	SizeBytes  int64
	AcceptedAt string
}

// AcceptCloudFunctionUpload records a source zip upload theatre (no build).
func (s *Store) AcceptCloudFunctionUpload(u CloudFunctionUpload) error {
	if u.UploadID == "" || u.ProjectID == "" || u.Location == "" {
		return fmt.Errorf("cloud function upload requires upload id, project, and location")
	}
	if u.AcceptedAt == "" {
		u.AcceptedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO cloud_function_uploads
		 (upload_id, project_id, location, bucket, object, size_bytes, accepted_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		u.UploadID, u.ProjectID, u.Location, u.Bucket, u.Object, u.SizeBytes, u.AcceptedAt,
	)
	if err != nil {
		return fmt.Errorf("accept cloud function upload: %w", err)
	}
	return nil
}

// GetCloudFunctionUpload loads an accepted upload by id.
func (s *Store) GetCloudFunctionUpload(uploadID string) (CloudFunctionUpload, bool, error) {
	var u CloudFunctionUpload
	err := s.db.QueryRow(
		`SELECT upload_id, project_id, location, bucket, object, size_bytes, accepted_at
		 FROM cloud_function_uploads WHERE upload_id = ?`, uploadID,
	).Scan(&u.UploadID, &u.ProjectID, &u.Location, &u.Bucket, &u.Object, &u.SizeBytes, &u.AcceptedAt)
	if err == sql.ErrNoRows {
		return CloudFunctionUpload{}, false, nil
	}
	if err != nil {
		return CloudFunctionUpload{}, false, fmt.Errorf("get cloud function upload: %w", err)
	}
	return u, true, nil
}

// HasCloudFunctionUploadObject reports whether a storageSource object was accepted.
func (s *Store) HasCloudFunctionUploadObject(projectID, bucket, object string) (bool, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(1) FROM cloud_function_uploads
		 WHERE project_id = ? AND bucket = ? AND object = ?`,
		projectID, bucket, object,
	).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// ActivateCloudFunctionsForStorageSource sets DEPLOYING functions with matching
// buildConfig.source.storageSource to ACTIVE after upload accept theatre.
func (s *Store) ActivateCloudFunctionsForStorageSource(projectID, location, bucket, object string) (int, error) {
	list, err := s.ListCloudFunctions(projectID, location)
	if err != nil {
		return 0, err
	}
	activated := 0
	for _, fn := range list {
		if fn.State != "DEPLOYING" {
			continue
		}
		b, o, ok := storageSourceFromConfigJSON(fn.ConfigJSON)
		if !ok || b != bucket || o != object {
			continue
		}
		if _, found, err := s.SetCloudFunctionState(fn.Name, "ACTIVE"); err != nil {
			return activated, err
		} else if found {
			activated++
		}
	}
	return activated, nil
}

func storageSourceFromConfigJSON(configJSON string) (bucket, object string, ok bool) {
	var cfg map[string]any
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return "", "", false
	}
	bc, _ := cfg["buildConfig"].(map[string]any)
	if bc == nil {
		return "", "", false
	}
	src, _ := bc["source"].(map[string]any)
	if src == nil {
		return "", "", false
	}
	ss, _ := src["storageSource"].(map[string]any)
	if ss == nil {
		return "", "", false
	}
	bucket, _ = ss["bucket"].(string)
	object, _ = ss["object"].(string)
	return bucket, object, bucket != "" && object != ""
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
	if job.NextRunTime == "" {
		job.NextRunTime = NextCronRunRFC3339(job.Schedule, job.TimeZone, time.Now().UTC())
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO scheduler_jobs
		 (name, project_id, location, job_id, schedule, time_zone, state, http_target_json, pubsub_target_json, oidc_audience, next_run_time, last_attempt_time, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.Name, job.ProjectID, job.Location, job.JobID, job.Schedule, job.TimeZone, job.State,
		job.HTTPTargetJSON, job.PubsubTargetJSON, job.OIDCAudience, job.NextRunTime, job.LastAttemptTime, job.CreatedAt, job.UpdatedAt,
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
		        oidc_audience, next_run_time, last_attempt_time, created_at, updated_at
		 FROM scheduler_jobs WHERE name = ?`, name,
	).Scan(
		&job.Name, &job.ProjectID, &job.Location, &job.JobID, &job.Schedule, &job.TimeZone, &job.State,
		&job.HTTPTargetJSON, &job.PubsubTargetJSON, &job.OIDCAudience, &job.NextRunTime, &job.LastAttemptTime, &job.CreatedAt, &job.UpdatedAt,
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
		        oidc_audience, next_run_time, last_attempt_time, created_at, updated_at
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
			&job.HTTPTargetJSON, &job.PubsubTargetJSON, &job.OIDCAudience, &job.NextRunTime, &job.LastAttemptTime, &job.CreatedAt, &job.UpdatedAt,
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
	if job.NextRunTime == "" {
		job.NextRunTime = NextCronRunRFC3339(job.Schedule, job.TimeZone, time.Now().UTC())
	}
	res, err := s.db.Exec(
		`UPDATE scheduler_jobs SET schedule = ?, time_zone = ?, state = ?, http_target_json = ?, pubsub_target_json = ?,
		 oidc_audience = ?, next_run_time = ?, last_attempt_time = ?, updated_at = ? WHERE name = ?`,
		job.Schedule, job.TimeZone, job.State, job.HTTPTargetJSON, job.PubsubTargetJSON,
		job.OIDCAudience, job.NextRunTime, job.LastAttemptTime, job.UpdatedAt, job.Name,
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

// MarkSchedulerJobAttempt sets last_attempt_time and refreshes next_run_time.
func (s *Store) MarkSchedulerJobAttempt(name string) error {
	job, ok, err := s.GetSchedulerJob(name)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	now := time.Now().UTC()
	next := NextCronRunRFC3339(job.Schedule, job.TimeZone, now)
	_, err = s.db.Exec(
		`UPDATE scheduler_jobs SET last_attempt_time = ?, next_run_time = ?, updated_at = ? WHERE name = ?`,
		now.Format(time.RFC3339Nano), next, now.Format(time.RFC3339Nano), name,
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
	if q.RateLimitsJSON == "" {
		q.RateLimitsJSON = `{"maxDispatchesPerSecond":500,"maxBurstSize":100,"maxConcurrentDispatches":1000}`
	}
	if q.RetryConfigJSON == "" {
		q.RetryConfigJSON = `{"maxAttempts":100,"maxRetryDuration":"0s","minBackoff":"0.100s","maxBackoff":"3600s","maxDoublings":16}`
	}
	if q.CreatedAt == "" {
		q.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO cloud_tasks_queues
		 (name, project_id, location, queue_id, state, rate_limits_json, retry_config_json, app_engine_routing_override_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		q.Name, q.ProjectID, q.Location, q.QueueID, q.State, q.RateLimitsJSON, q.RetryConfigJSON, q.AppEngineRoutingOverrideJSON, q.CreatedAt,
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
		`SELECT name, project_id, location, queue_id, state, rate_limits_json, retry_config_json, app_engine_routing_override_json, created_at
		 FROM cloud_tasks_queues WHERE name = ?`, name,
	).Scan(&q.Name, &q.ProjectID, &q.Location, &q.QueueID, &q.State, &q.RateLimitsJSON, &q.RetryConfigJSON, &q.AppEngineRoutingOverrideJSON, &q.CreatedAt)
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
		`SELECT name, project_id, location, queue_id, state, rate_limits_json, retry_config_json, app_engine_routing_override_json, created_at
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
		if err := rows.Scan(&q.Name, &q.ProjectID, &q.Location, &q.QueueID, &q.State, &q.RateLimitsJSON, &q.RetryConfigJSON, &q.AppEngineRoutingOverrideJSON, &q.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// UpdateCloudTasksQueue patches rate limits, retry config, routing override, and state.
func (s *Store) UpdateCloudTasksQueue(q CloudTasksQueue) (bool, error) {
	res, err := s.db.Exec(
		`UPDATE cloud_tasks_queues SET state = ?, rate_limits_json = ?, retry_config_json = ?, app_engine_routing_override_json = ?
		 WHERE name = ?`,
		q.State, q.RateLimitsJSON, q.RetryConfigJSON, q.AppEngineRoutingOverrideJSON, q.Name,
	)
	if err != nil {
		return false, fmt.Errorf("update cloud tasks queue: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
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
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO cloud_tasks
		 (name, queue_name, schedule_time, create_time, http_request_json, app_engine_http_request_json, dispatch_count, response_count)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		task.Name, task.QueueName, task.ScheduleTime, task.CreateTime, task.HTTPRequestJSON, task.AppEngineHTTPRequestJSON, task.DispatchCount, task.ResponseCount,
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
		`SELECT name, queue_name, schedule_time, create_time, http_request_json, app_engine_http_request_json, dispatch_count, response_count
		 FROM cloud_tasks WHERE name = ?`, name,
	).Scan(&t.Name, &t.QueueName, &t.ScheduleTime, &t.CreateTime, &t.HTTPRequestJSON, &t.AppEngineHTTPRequestJSON, &t.DispatchCount, &t.ResponseCount)
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
		`SELECT name, queue_name, schedule_time, create_time, http_request_json, app_engine_http_request_json, dispatch_count, response_count
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
		if err := rows.Scan(&t.Name, &t.QueueName, &t.ScheduleTime, &t.CreateTime, &t.HTTPRequestJSON, &t.AppEngineHTTPRequestJSON, &t.DispatchCount, &t.ResponseCount); err != nil {
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

// IncrementCloudTaskResponse bumps response_count after a dispatch attempt completes.
func (s *Store) IncrementCloudTaskResponse(name string) error {
	_, err := s.db.Exec(`UPDATE cloud_tasks SET response_count = response_count + 1 WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("increment task response: %w", err)
	}
	return nil
}

// SchedulerOIDCAudience returns oidcToken.audience from httpTarget JSON when present.
func SchedulerOIDCAudience(httpTargetJSON string) string {
	if strings.TrimSpace(httpTargetJSON) == "" {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(httpTargetJSON), &m); err != nil {
		return ""
	}
	oidc, ok := m["oidcToken"].(map[string]any)
	if !ok {
		return ""
	}
	aud, _ := oidc["audience"].(string)
	return aud
}

// HTTPAuthServiceAccountEmail extracts oidcToken or oauthToken serviceAccountEmail from HTTP target JSON.
func HTTPAuthServiceAccountEmail(httpJSON string) string {
	if strings.TrimSpace(httpJSON) == "" {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(httpJSON), &m); err != nil {
		return ""
	}
	for _, key := range []string{"oidcToken", "oauthToken"} {
		tok, ok := m[key].(map[string]any)
		if !ok {
			continue
		}
		if email, _ := tok["serviceAccountEmail"].(string); strings.TrimSpace(email) != "" {
			return strings.TrimSpace(email)
		}
	}
	return ""
}

// NextCronRunRFC3339 best-effort next run for a 5-field cron (minute hour dom mon dow).
// Returns empty string when the expression cannot be evaluated.
func NextCronRunRFC3339(schedule, timeZone string, from time.Time) string {
	next, ok := nextCronRun(schedule, timeZone, from)
	if !ok {
		return ""
	}
	return next.UTC().Format(time.RFC3339Nano)
}

func nextCronRun(schedule, timeZone string, from time.Time) (time.Time, bool) {
	fields := strings.Fields(strings.TrimSpace(schedule))
	if len(fields) != 5 {
		return time.Time{}, false
	}
	loc := time.UTC
	if timeZone != "" && !strings.EqualFold(timeZone, "UTC") {
		if l, err := time.LoadLocation(timeZone); err == nil {
			loc = l
		}
	}
	t := from.In(loc).Add(time.Minute).Truncate(time.Minute)
	// Scan up to ~2 years of minutes.
	for i := 0; i < 60*24*366*2; i++ {
		if cronFieldMatch(fields[0], t.Minute(), 0, 59) &&
			cronFieldMatch(fields[1], t.Hour(), 0, 23) &&
			cronFieldMatch(fields[2], t.Day(), 1, 31) &&
			cronFieldMatch(fields[3], int(t.Month()), 1, 12) &&
			cronFieldMatch(fields[4], int(t.Weekday()), 0, 6) {
			return t, true
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}, false
}

func cronFieldMatch(field string, value, min, max int) bool {
	field = strings.TrimSpace(field)
	if field == "*" || field == "?" {
		return true
	}
	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		step := 1
		base := part
		if strings.Contains(part, "/") {
			bits := strings.SplitN(part, "/", 2)
			base = bits[0]
			if _, err := fmt.Sscanf(bits[1], "%d", &step); err != nil || step < 1 {
				return false
			}
		}
		if base == "*" {
			if (value-min)%step == 0 {
				return true
			}
			continue
		}
		if strings.Contains(base, "-") {
			var a, b int
			if _, err := fmt.Sscanf(base, "%d-%d", &a, &b); err != nil {
				return false
			}
			if value >= a && value <= b && (value-a)%step == 0 {
				return true
			}
			continue
		}
		var n int
		if _, err := fmt.Sscanf(base, "%d", &n); err != nil {
			return false
		}
		if step > 1 {
			if value >= n && (value-n)%step == 0 && value <= max {
				return true
			}
			continue
		}
		if n == value {
			return true
		}
	}
	return false
}
