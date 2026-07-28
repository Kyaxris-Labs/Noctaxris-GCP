package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// --- Monitoring ---

// MetricDescriptorRow is a Cloud Monitoring metric descriptor.
type MetricDescriptorRow struct {
	Name        string
	ProjectID   string
	Type        string
	MetricKind  string
	ValueType   string
	Description string
	DisplayName string
	LabelsJSON  string
	CreatedAt   string
}

// CreateMetricDescriptor inserts a descriptor. created=false means type exists.
func (s *Store) CreateMetricDescriptor(d MetricDescriptorRow) (*MetricDescriptorRow, bool, error) {
	d.ProjectID = strings.TrimSpace(d.ProjectID)
	d.Type = strings.TrimSpace(d.Type)
	if d.ProjectID == "" || d.Type == "" {
		return nil, false, fmt.Errorf("project and type required")
	}
	if d.Name == "" {
		d.Name = "projects/" + d.ProjectID + "/metricDescriptors/" + d.Type
	}
	if d.MetricKind == "" {
		d.MetricKind = "GAUGE"
	}
	if d.ValueType == "" {
		d.ValueType = "DOUBLE"
	}
	if d.LabelsJSON == "" {
		d.LabelsJSON = "[]"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	d.CreatedAt = now
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO metric_descriptors
		 (name, project_id, type, metric_kind, value_type, description, display_name, labels_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.Name, d.ProjectID, d.Type, d.MetricKind, d.ValueType, d.Description, d.DisplayName, d.LabelsJSON, now,
	)
	if err != nil {
		return nil, false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	if n == 0 {
		return nil, false, nil
	}
	return &d, true, nil
}

// GetMetricDescriptor loads by full resource name or type under project.
func (s *Store) GetMetricDescriptor(projectID, typeOrName string) (*MetricDescriptorRow, bool, error) {
	var d MetricDescriptorRow
	err := s.db.QueryRow(
		`SELECT name, project_id, type, metric_kind, value_type, description, display_name, labels_json, created_at
		 FROM metric_descriptors WHERE project_id = ? AND (type = ? OR name = ?)`,
		projectID, typeOrName, typeOrName,
	).Scan(&d.Name, &d.ProjectID, &d.Type, &d.MetricKind, &d.ValueType, &d.Description, &d.DisplayName, &d.LabelsJSON, &d.CreatedAt)
	if err == sql.ErrNoRows {
		// try full name without project filter
		err = s.db.QueryRow(
			`SELECT name, project_id, type, metric_kind, value_type, description, display_name, labels_json, created_at
			 FROM metric_descriptors WHERE name = ?`,
			typeOrName,
		).Scan(&d.Name, &d.ProjectID, &d.Type, &d.MetricKind, &d.ValueType, &d.Description, &d.DisplayName, &d.LabelsJSON, &d.CreatedAt)
		if err == sql.ErrNoRows {
			return nil, false, nil
		}
	}
	if err != nil {
		return nil, false, err
	}
	return &d, true, nil
}

// ListMetricDescriptors lists descriptors for a project.
func (s *Store) ListMetricDescriptors(projectID string) ([]MetricDescriptorRow, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, type, metric_kind, value_type, description, display_name, labels_json, created_at
		 FROM metric_descriptors WHERE project_id = ? ORDER BY type`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MetricDescriptorRow
	for rows.Next() {
		var d MetricDescriptorRow
		if err := rows.Scan(&d.Name, &d.ProjectID, &d.Type, &d.MetricKind, &d.ValueType, &d.Description, &d.DisplayName, &d.LabelsJSON, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// TimeSeriesPoint is one Monitoring data point.
type TimeSeriesPoint struct {
	ID                 string
	ProjectID          string
	MetricType         string
	ResourceType       string
	ResourceLabelsJSON string
	MetricLabelsJSON   string
	EndTime            string
	StartTime          string
	ValueJSON          string
	CreatedAt          string
}

// CreateTimeSeriesPoints inserts points.
func (s *Store) CreateTimeSeriesPoints(points []TimeSeriesPoint) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, p := range points {
		if p.ID == "" {
			p.ID = uuid.NewString()
		}
		if p.ResourceType == "" {
			p.ResourceType = "global"
		}
		if p.ResourceLabelsJSON == "" {
			p.ResourceLabelsJSON = "{}"
		}
		if p.MetricLabelsJSON == "" {
			p.MetricLabelsJSON = "{}"
		}
		if p.EndTime == "" {
			p.EndTime = now
		}
		if p.ValueJSON == "" {
			p.ValueJSON = "{}"
		}
		if _, err := tx.Exec(
			`INSERT INTO time_series_points
			 (id, project_id, metric_type, resource_type, resource_labels_json, metric_labels_json, end_time, start_time, value_json, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			p.ID, p.ProjectID, p.MetricType, p.ResourceType, p.ResourceLabelsJSON, p.MetricLabelsJSON,
			p.EndTime, p.StartTime, p.ValueJSON, now,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListTimeSeriesFilter selects points for ListTimeSeries.
type ListTimeSeriesFilter struct {
	ProjectID  string
	MetricType string
	StartTime  string
	EndTime    string
}

// ListTimeSeriesPoints returns points ordered by end_time.
func (s *Store) ListTimeSeriesPoints(f ListTimeSeriesFilter) ([]TimeSeriesPoint, error) {
	q := `SELECT id, project_id, metric_type, resource_type, resource_labels_json, metric_labels_json, end_time, start_time, value_json, created_at
	      FROM time_series_points WHERE project_id = ?`
	args := []any{f.ProjectID}
	if f.MetricType != "" {
		q += ` AND metric_type = ?`
		args = append(args, f.MetricType)
	}
	if f.StartTime != "" {
		q += ` AND end_time >= ?`
		args = append(args, f.StartTime)
	}
	if f.EndTime != "" {
		q += ` AND end_time <= ?`
		args = append(args, f.EndTime)
	}
	q += ` ORDER BY end_time`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TimeSeriesPoint
	for rows.Next() {
		var p TimeSeriesPoint
		if err := rows.Scan(&p.ID, &p.ProjectID, &p.MetricType, &p.ResourceType, &p.ResourceLabelsJSON, &p.MetricLabelsJSON, &p.EndTime, &p.StartTime, &p.ValueJSON, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// DeleteTimeSeriesPoints deletes points matching project and optional metric type. Returns rows deleted.
func (s *Store) DeleteTimeSeriesPoints(projectID, metricType string) (int64, error) {
	if projectID == "" {
		return 0, fmt.Errorf("project id required")
	}
	var (
		res sql.Result
		err error
	)
	if metricType == "" {
		res, err = s.db.Exec(`DELETE FROM time_series_points WHERE project_id = ?`, projectID)
	} else {
		res, err = s.db.Exec(`DELETE FROM time_series_points WHERE project_id = ? AND metric_type = ?`, projectID, metricType)
	}
	if err != nil {
		return 0, fmt.Errorf("delete time series: %w", err)
	}
	return res.RowsAffected()
}

// AlertPolicyRow is a Cloud Monitoring alert policy metadata row (theatre).
type AlertPolicyRow struct {
	Name               string
	ProjectID          string
	PolicyID           string
	DisplayName        string
	Enabled            bool
	Combiner           string
	ConditionsJSON     string
	DocumentationJSON  string
	UserLabelsJSON     string
	CreatedAt          string
	UpdatedAt          string
}

// CreateAlertPolicy inserts an alert policy. created=false when already exists.
func (s *Store) CreateAlertPolicy(p AlertPolicyRow) (*AlertPolicyRow, bool, error) {
	p.ProjectID = strings.TrimSpace(p.ProjectID)
	p.PolicyID = strings.TrimSpace(p.PolicyID)
	if p.ProjectID == "" || p.PolicyID == "" {
		return nil, false, fmt.Errorf("project and policy id required")
	}
	if p.Name == "" {
		p.Name = "projects/" + p.ProjectID + "/alertPolicies/" + p.PolicyID
	}
	if p.Combiner == "" {
		p.Combiner = "OR"
	}
	if p.ConditionsJSON == "" {
		p.ConditionsJSON = "[]"
	}
	if p.DocumentationJSON == "" {
		p.DocumentationJSON = "{}"
	}
	if p.UserLabelsJSON == "" {
		p.UserLabelsJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	p.CreatedAt = now
	p.UpdatedAt = now
	enabled := 1
	if !p.Enabled {
		enabled = 0
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO alert_policies
		 (name, project_id, policy_id, display_name, enabled, combiner, conditions_json, documentation_json, user_labels_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.ProjectID, p.PolicyID, p.DisplayName, enabled, p.Combiner, p.ConditionsJSON, p.DocumentationJSON, p.UserLabelsJSON, now, now,
	)
	if err != nil {
		return nil, false, fmt.Errorf("create alert policy: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	if n == 0 {
		return nil, false, nil
	}
	return &p, true, nil
}

// GetAlertPolicy loads by resource name.
func (s *Store) GetAlertPolicy(name string) (*AlertPolicyRow, bool, error) {
	var p AlertPolicyRow
	var enabled int
	err := s.db.QueryRow(
		`SELECT name, project_id, policy_id, display_name, enabled, combiner, conditions_json, documentation_json, user_labels_json, created_at, updated_at
		 FROM alert_policies WHERE name = ?`, name,
	).Scan(&p.Name, &p.ProjectID, &p.PolicyID, &p.DisplayName, &enabled, &p.Combiner, &p.ConditionsJSON, &p.DocumentationJSON, &p.UserLabelsJSON, &p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("get alert policy: %w", err)
	}
	p.Enabled = enabled != 0
	return &p, true, nil
}

// ListAlertPolicies lists policies for a project.
func (s *Store) ListAlertPolicies(projectID string) ([]AlertPolicyRow, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, policy_id, display_name, enabled, combiner, conditions_json, documentation_json, user_labels_json, created_at, updated_at
		 FROM alert_policies WHERE project_id = ? ORDER BY policy_id`,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("list alert policies: %w", err)
	}
	defer rows.Close()
	var out []AlertPolicyRow
	for rows.Next() {
		var p AlertPolicyRow
		var enabled int
		if err := rows.Scan(&p.Name, &p.ProjectID, &p.PolicyID, &p.DisplayName, &enabled, &p.Combiner, &p.ConditionsJSON, &p.DocumentationJSON, &p.UserLabelsJSON, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.Enabled = enabled != 0
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpdateAlertPolicy replaces theatre metadata for an existing policy.
func (s *Store) UpdateAlertPolicy(p AlertPolicyRow) (*AlertPolicyRow, bool, error) {
	if p.Name == "" {
		return nil, false, fmt.Errorf("name required")
	}
	if p.Combiner == "" {
		p.Combiner = "OR"
	}
	if p.ConditionsJSON == "" {
		p.ConditionsJSON = "[]"
	}
	if p.DocumentationJSON == "" {
		p.DocumentationJSON = "{}"
	}
	if p.UserLabelsJSON == "" {
		p.UserLabelsJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	enabled := 1
	if !p.Enabled {
		enabled = 0
	}
	res, err := s.db.Exec(
		`UPDATE alert_policies SET display_name = ?, enabled = ?, combiner = ?, conditions_json = ?, documentation_json = ?, user_labels_json = ?, updated_at = ?
		 WHERE name = ?`,
		p.DisplayName, enabled, p.Combiner, p.ConditionsJSON, p.DocumentationJSON, p.UserLabelsJSON, now, p.Name,
	)
	if err != nil {
		return nil, false, fmt.Errorf("update alert policy: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	if n == 0 {
		return nil, false, nil
	}
	return s.GetAlertPolicy(p.Name)
}

// DeleteAlertPolicy removes a policy by name.
func (s *Store) DeleteAlertPolicy(name string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM alert_policies WHERE name = ?`, name)
	if err != nil {
		return false, fmt.Errorf("delete alert policy: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
