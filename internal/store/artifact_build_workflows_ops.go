package store

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// PatchArRepositoryDeepen updates description and/or labels when the corresponding pointer is non-nil.
func (s *Store) PatchArRepositoryDeepen(name string, description *string, labelsJSON *string) (ArRepository, bool, error) {
	cur, ok, err := s.GetArRepository(name)
	if err != nil || !ok {
		return ArRepository{}, ok, err
	}
	if description != nil {
		cur.Description = *description
	}
	if labelsJSON != nil {
		if *labelsJSON == "" {
			cur.LabelsJSON = "{}"
		} else {
			cur.LabelsJSON = *labelsJSON
		}
	}
	cur.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.Exec(
		`UPDATE ar_repositories SET description = ?, labels_json = ?, updated_at = ? WHERE name = ?`,
		cur.Description, cur.LabelsJSON, cur.UpdatedAt, name,
	)
	if err != nil {
		return ArRepository{}, false, fmt.Errorf("patch ar repository deepen: %w", err)
	}
	return cur, true, nil
}

// ArFileTheatre is synthetic Artifact Registry file metadata (no blob bytes).
type ArFileTheatre struct {
	Name      string
	SizeBytes string
	Owner     string
	CreateTime string
	UpdateTime string
}

// ListArFilesTheatreDeepen synthesizes file rows from package versions under a repository.
func (s *Store) ListArFilesTheatreDeepen(repositoryName string) ([]ArFileTheatre, error) {
	pkgs, err := s.ListArPackages(repositoryName)
	if err != nil {
		return nil, err
	}
	var out []ArFileTheatre
	for _, pkg := range pkgs {
		vers, err := s.ListArVersions(pkg.Name)
		if err != nil {
			return nil, err
		}
		for _, v := range vers {
			// URL-safe-ish file id from version id (colon → %3A theatre).
			fileID := strings.ReplaceAll(v.VersionID, ":", "%3A") + ".lab"
			out = append(out, ArFileTheatre{
				Name:       repositoryName + "/files/" + fileID,
				SizeBytes:  "0",
				Owner:      v.Name,
				CreateTime: v.CreatedAt,
				UpdateTime: v.UpdatedAt,
			})
		}
	}
	return out, nil
}

// ArTagTheatre is synthetic Artifact Registry tag metadata.
type ArTagTheatre struct {
	Name    string
	Version string
}

// ListArTagsTheatreDeepen lists tags derived from version relatedTagsJSON under a package.
func (s *Store) ListArTagsTheatreDeepen(packageName string) ([]ArTagTheatre, error) {
	vers, err := s.ListArVersions(packageName)
	if err != nil {
		return nil, err
	}
	var out []ArTagTheatre
	for _, v := range vers {
		var tags []any
		_ = json.Unmarshal([]byte(v.RelatedTagsJSON), &tags)
		for _, t := range tags {
			switch tv := t.(type) {
			case string:
				if tv == "" {
					continue
				}
				out = append(out, ArTagTheatre{
					Name:    packageName + "/tags/" + tv,
					Version: v.Name,
				})
			case map[string]any:
				name, _ := tv["name"].(string)
				if name == "" {
					continue
				}
				// relatedTags may store short tag name or full resource name.
				tagID := name
				if i := strings.LastIndex(name, "/tags/"); i >= 0 {
					tagID = name[i+len("/tags/"):]
				}
				ver := v.Name
				if vn, _ := tv["version"].(string); vn != "" {
					ver = vn
				}
				out = append(out, ArTagTheatre{
					Name:    packageName + "/tags/" + tagID,
					Version: ver,
				})
			}
		}
	}
	return out, nil
}

// CancelCbBuildDeepen marks a non-terminal build CANCELLED. Returns false if missing.
func (s *Store) CancelCbBuildDeepen(name string) (CbBuild, bool, error) {
	b, ok, err := s.GetCbBuild(name)
	if err != nil || !ok {
		return CbBuild{}, ok, err
	}
	switch b.Status {
	case "SUCCESS", "FAILURE", "INTERNAL_ERROR", "TIMEOUT", "CANCELLED", "EXPIRED":
		return b, true, nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	b.Status = "CANCELLED"
	b.StatusDetail = "lab theatre: build cancelled"
	b.FinishTime = now
	b.BuildJSON = markCbBuildStepsStatus(b.BuildJSON, "CANCELLED")
	_, err = s.db.Exec(
		`UPDATE cb_builds SET status = ?, status_detail = ?, finish_time = ?, build_json = ? WHERE name = ?`,
		b.Status, b.StatusDetail, b.FinishTime, b.BuildJSON, name,
	)
	if err != nil {
		return CbBuild{}, false, fmt.Errorf("cancel cb build deepen: %w", err)
	}
	return b, true, nil
}

// PatchWorkflowDeepen updates workflow fields when pointers are non-nil and bumps revision when source changes.
func (s *Store) PatchWorkflowDeepen(name string, description, sourceContents, serviceAccount, labelsJSON *string) (Workflow, bool, error) {
	cur, ok, err := s.GetWorkflow(name)
	if err != nil || !ok {
		return Workflow{}, ok, err
	}
	sourceChanged := false
	if description != nil {
		cur.Description = *description
	}
	if sourceContents != nil {
		if *sourceContents != cur.SourceContents {
			sourceChanged = true
		}
		cur.SourceContents = *sourceContents
	}
	if serviceAccount != nil {
		cur.ServiceAccount = *serviceAccount
	}
	if labelsJSON != nil {
		if *labelsJSON == "" {
			cur.LabelsJSON = "{}"
		} else {
			cur.LabelsJSON = *labelsJSON
		}
	}
	if sourceChanged {
		cur.RevisionID = bumpWorkflowRevisionDeepen(cur.RevisionID)
	}
	cur.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.Exec(
		`UPDATE wf_workflows SET description = ?, source_contents = ?, service_account = ?,
		 labels_json = ?, revision_id = ?, updated_at = ? WHERE name = ?`,
		cur.Description, cur.SourceContents, cur.ServiceAccount, cur.LabelsJSON, cur.RevisionID, cur.UpdatedAt, name,
	)
	if err != nil {
		return Workflow{}, false, fmt.Errorf("patch workflow deepen: %w", err)
	}
	return cur, true, nil
}

func bumpWorkflowRevisionDeepen(cur string) string {
	// Theatre: 000001-lab → 000002-lab
	n := 1
	if parts := strings.SplitN(cur, "-", 2); len(parts) > 0 {
		if v, err := strconv.Atoi(parts[0]); err == nil {
			n = v + 1
		}
	}
	return fmt.Sprintf("%06d-lab", n)
}

// CancelWorkflowExecutionDeepen sets ACTIVE executions to CANCELLED. Terminal states are returned unchanged.
func (s *Store) CancelWorkflowExecutionDeepen(name string) (WorkflowExecution, bool, error) {
	e, ok, err := s.GetWorkflowExecution(name)
	if err != nil || !ok {
		return WorkflowExecution{}, ok, err
	}
	if e.State != "ACTIVE" && e.State != "QUEUED" {
		return e, true, nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	e.State = "CANCELLED"
	e.EndTime = now
	e.Result = `{"ok":false,"lab":"noctaxris-gcp-workflows","cancelled":true}`
	_, err = s.db.Exec(
		`UPDATE wf_executions SET state = ?, end_time = ?, result = ? WHERE name = ?`,
		e.State, e.EndTime, e.Result, name,
	)
	if err != nil {
		return WorkflowExecution{}, false, fmt.Errorf("cancel workflow execution deepen: %w", err)
	}
	return e, true, nil
}

// AdvanceWorkflowExecutionDeepen flips ACTIVE executions to SUCCEEDED theatre on read.
func (s *Store) AdvanceWorkflowExecutionDeepen(name string) (WorkflowExecution, bool, error) {
	e, ok, err := s.GetWorkflowExecution(name)
	if err != nil || !ok {
		return WorkflowExecution{}, ok, err
	}
	if e.State != "ACTIVE" && e.State != "QUEUED" {
		return e, true, nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	e.State = "SUCCEEDED"
	e.EndTime = now
	if e.Result == "" {
		if e.Argument != "" {
			e.Result = fmt.Sprintf(`{"ok":true,"lab":"noctaxris-gcp-workflows","argument":%s}`, e.Argument)
		} else {
			e.Result = `{"ok":true,"lab":"noctaxris-gcp-workflows"}`
		}
	}
	_, err = s.db.Exec(
		`UPDATE wf_executions SET state = ?, end_time = ?, result = ? WHERE name = ?`,
		e.State, e.EndTime, e.Result, name,
	)
	if err != nil {
		return WorkflowExecution{}, false, fmt.Errorf("advance workflow execution deepen: %w", err)
	}
	return e, true, nil
}

// ListWorkflowExecutionsPageDeepen lists executions with optional pageSize and pageToken (offset string).
func (s *Store) ListWorkflowExecutionsPageDeepen(workflowName string, pageSize int, pageToken string) ([]WorkflowExecution, string, error) {
	if pageSize <= 0 {
		pageSize = 100
	}
	if pageSize > 1000 {
		pageSize = 1000
	}
	offset := 0
	if pageToken != "" {
		n, err := strconv.Atoi(pageToken)
		if err != nil || n < 0 {
			return nil, "", fmt.Errorf("invalid pageToken")
		}
		offset = n
	}
	rows, err := s.db.Query(
		`SELECT name, workflow_name, project_id, location, workflow_id, execution_id, argument, result,
		        state, workflow_revision_id, created_at, start_time, end_time
		 FROM wf_executions WHERE workflow_name = ? ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		workflowName, pageSize+1, offset,
	)
	if err != nil {
		return nil, "", fmt.Errorf("list workflow executions page deepen: %w", err)
	}
	defer rows.Close()
	var out []WorkflowExecution
	for rows.Next() {
		var e WorkflowExecution
		if err := rows.Scan(
			&e.Name, &e.WorkflowName, &e.ProjectID, &e.Location, &e.WorkflowID, &e.ExecutionID, &e.Argument, &e.Result,
			&e.State, &e.WorkflowRevisionID, &e.CreatedAt, &e.StartTime, &e.EndTime,
		); err != nil {
			return nil, "", err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > pageSize {
		next = strconv.Itoa(offset + pageSize)
		out = out[:pageSize]
	}
	return out, next, nil
}

// ListWorkflowsPageDeepen lists workflows with optional pageSize and pageToken (offset string).
func (s *Store) ListWorkflowsPageDeepen(projectID, location string, pageSize int, pageToken string) ([]Workflow, string, error) {
	if pageSize <= 0 {
		pageSize = 100
	}
	if pageSize > 1000 {
		pageSize = 1000
	}
	offset := 0
	if pageToken != "" {
		n, err := strconv.Atoi(pageToken)
		if err != nil || n < 0 {
			return nil, "", fmt.Errorf("invalid pageToken")
		}
		offset = n
	}
	rows, err := s.db.Query(
		`SELECT name, project_id, location, workflow_id, description, source_contents, service_account,
		        revision_id, state, labels_json, created_at, updated_at
		 FROM wf_workflows WHERE project_id = ? AND location = ? ORDER BY name LIMIT ? OFFSET ?`,
		projectID, location, pageSize+1, offset,
	)
	if err != nil {
		return nil, "", fmt.Errorf("list workflows page deepen: %w", err)
	}
	defer rows.Close()
	var out []Workflow
	for rows.Next() {
		var w Workflow
		if err := rows.Scan(
			&w.Name, &w.ProjectID, &w.Location, &w.WorkflowID, &w.Description, &w.SourceContents, &w.ServiceAccount,
			&w.RevisionID, &w.State, &w.LabelsJSON, &w.CreatedAt, &w.UpdatedAt,
		); err != nil {
			return nil, "", err
		}
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > pageSize {
		next = strconv.Itoa(offset + pageSize)
		out = out[:pageSize]
	}
	return out, next, nil
}
