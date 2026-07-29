package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const cloudassetSchema = `
CREATE TABLE IF NOT EXISTS cloudasset_feeds (
  name TEXT PRIMARY KEY,
  parent TEXT NOT NULL,
  feed_id TEXT NOT NULL,
  asset_types_json TEXT NOT NULL DEFAULT '[]',
  content_type TEXT NOT NULL DEFAULT 'RESOURCE',
  pubsub_topic TEXT NOT NULL DEFAULT '',
  body_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE (parent, feed_id)
);

CREATE INDEX IF NOT EXISTS idx_cloudasset_feeds_parent
  ON cloudasset_feeds (parent);

CREATE TABLE IF NOT EXISTS cloudasset_asset_history (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  parent TEXT NOT NULL,
  asset_name TEXT NOT NULL,
  asset_type TEXT NOT NULL,
  content_json TEXT NOT NULL DEFAULT '{}',
  window_start TEXT NOT NULL DEFAULT '',
  window_end TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_cloudasset_history_parent_name
  ON cloudasset_asset_history (parent, asset_name);
`

func (s *Store) ensureCloudAssetSchema() error {
	if _, err := s.db.Exec(cloudassetSchema); err != nil {
		return fmt.Errorf("migrate cloudasset: %w", err)
	}
	return nil
}

// InventoryAsset is a lab Cloud Asset Inventory row synthesized from store resources.
type InventoryAsset struct {
	Name        string
	AssetType   string
	ProjectID   string
	Location    string
	DisplayName string
	CreateTime  string
	UpdateTime  string
	State       string
	LabelsJSON  string
	DataJSON    string
}

// CloudAssetFeed is a Cloud Asset Inventory feed row (theatre).
type CloudAssetFeed struct {
	Name           string
	Parent         string
	FeedID         string
	AssetTypesJSON string
	ContentType    string
	PubsubTopic    string
	BodyJSON       string
	CreatedAt      string
	UpdatedAt      string
}

// CloudAssetHistoryRow is a TemporalAsset theatre row for batchGetAssetsHistory.
type CloudAssetHistoryRow struct {
	ID          int64
	Parent      string
	AssetName   string
	AssetType   string
	ContentJSON string
	WindowStart string
	WindowEnd   string
	CreatedAt   string
}

// ListInventoryAssets returns project-scoped assets from existing store tables.
// Includes the project, buckets, Pub/Sub topics, and service accounts when present.
func (s *Store) ListInventoryAssets(projectID string) ([]InventoryAsset, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, fmt.Errorf("project id required")
	}
	var out []InventoryAsset

	p, ok, err := s.GetProject(projectID)
	if err != nil {
		return nil, err
	}
	if ok {
		labels := p.LabelsJSON
		if labels == "" {
			labels = "{}"
		}
		data, _ := json.Marshal(map[string]any{
			"name":        "projects/" + p.ID,
			"projectId":   p.ID,
			"displayName": p.DisplayName,
			"state":       p.State,
			"createTime":  p.CreatedAt,
		})
		out = append(out, InventoryAsset{
			Name:        "//cloudresourcemanager.googleapis.com/projects/" + p.ID,
			AssetType:   "cloudresourcemanager.googleapis.com/Project",
			ProjectID:   p.ID,
			Location:    "global",
			DisplayName: p.DisplayName,
			CreateTime:  p.CreatedAt,
			UpdateTime:  p.CreatedAt,
			State:       p.State,
			LabelsJSON:  labels,
			DataJSON:    string(data),
		})
	}

	buckets, err := s.ListBuckets(projectID)
	if err != nil {
		return nil, err
	}
	for _, b := range buckets {
		labelsJSON := "{}"
		if len(b.Labels) > 0 {
			raw, _ := json.Marshal(b.Labels)
			labelsJSON = string(raw)
		}
		updated := b.UpdatedAt
		if updated == "" {
			updated = b.CreatedAt
		}
		data, _ := json.Marshal(map[string]any{
			"name":         b.Name,
			"id":           b.Name,
			"location":     b.Location,
			"storageClass": b.StorageClass,
			"timeCreated":  b.CreatedAt,
			"updated":      updated,
		})
		out = append(out, InventoryAsset{
			Name:        "//storage.googleapis.com/projects/_/buckets/" + b.Name,
			AssetType:   "storage.googleapis.com/Bucket",
			ProjectID:   projectID,
			Location:    b.Location,
			DisplayName: b.Name,
			CreateTime:  b.CreatedAt,
			UpdateTime:  updated,
			State:       "ACTIVE",
			LabelsJSON:  labelsJSON,
			DataJSON:    string(data),
		})
	}

	topics, err := s.ListTopics(projectID)
	if err != nil {
		return nil, err
	}
	for _, t := range topics {
		labelsJSON := "{}"
		if len(t.Labels) > 0 {
			raw, _ := json.Marshal(t.Labels)
			labelsJSON = string(raw)
		}
		short := t.Name
		if i := strings.LastIndex(t.Name, "/"); i >= 0 {
			short = t.Name[i+1:]
		}
		data, _ := json.Marshal(map[string]any{
			"name":   t.Name,
			"labels": t.Labels,
		})
		out = append(out, InventoryAsset{
			Name:        "//pubsub.googleapis.com/" + strings.TrimPrefix(t.Name, "/"),
			AssetType:   "pubsub.googleapis.com/Topic",
			ProjectID:   projectID,
			Location:    "global",
			DisplayName: short,
			CreateTime:  t.CreatedAt,
			UpdateTime:  t.CreatedAt,
			State:       "ACTIVE",
			LabelsJSON:  labelsJSON,
			DataJSON:    string(data),
		})
	}

	sas, err := s.ListServiceAccounts(projectID)
	if err != nil {
		return nil, err
	}
	for _, sa := range sas {
		state := "ACTIVE"
		if sa.Disabled {
			state = "DISABLED"
		}
		data, _ := json.Marshal(map[string]any{
			"name":        "projects/" + sa.ProjectID + "/serviceAccounts/" + sa.Email,
			"email":       sa.Email,
			"uniqueId":    sa.UniqueID,
			"displayName": sa.DisplayName,
			"disabled":    sa.Disabled,
		})
		out = append(out, InventoryAsset{
			Name:        "//iam.googleapis.com/projects/" + sa.ProjectID + "/serviceAccounts/" + sa.Email,
			AssetType:   "iam.googleapis.com/ServiceAccount",
			ProjectID:   projectID,
			Location:    "global",
			DisplayName: sa.DisplayName,
			CreateTime:  sa.CreatedAt,
			UpdateTime:  sa.CreatedAt,
			State:       state,
			LabelsJSON:  "{}",
			DataJSON:    string(data),
		})
	}

	return out, nil
}

// CreateCloudAssetFeed inserts a feed. Returns false when the feed id already exists under parent.
func (s *Store) CreateCloudAssetFeed(f CloudAssetFeed) (CloudAssetFeed, bool, error) {
	if err := s.ensureCloudAssetSchema(); err != nil {
		return CloudAssetFeed{}, false, err
	}
	f.Parent = strings.TrimSpace(f.Parent)
	f.FeedID = strings.TrimSpace(f.FeedID)
	if f.Parent == "" || f.FeedID == "" {
		return CloudAssetFeed{}, false, fmt.Errorf("parent and feed id required")
	}
	if f.Name == "" {
		f.Name = strings.TrimSuffix(f.Parent, "/") + "/feeds/" + f.FeedID
	}
	if f.AssetTypesJSON == "" {
		f.AssetTypesJSON = "[]"
	}
	if f.ContentType == "" {
		f.ContentType = "RESOURCE"
	}
	if f.BodyJSON == "" {
		f.BodyJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if f.CreatedAt == "" {
		f.CreatedAt = now
	}
	f.UpdatedAt = now
	_, err := s.db.Exec(
		`INSERT INTO cloudasset_feeds
		 (name, parent, feed_id, asset_types_json, content_type, pubsub_topic, body_json, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.Name, f.Parent, f.FeedID, f.AssetTypesJSON, f.ContentType, f.PubsubTopic, f.BodyJSON, f.CreatedAt, f.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return CloudAssetFeed{}, false, nil
		}
		return CloudAssetFeed{}, false, err
	}
	return f, true, nil
}

// GetCloudAssetFeed loads a feed by resource name.
func (s *Store) GetCloudAssetFeed(name string) (CloudAssetFeed, bool, error) {
	if err := s.ensureCloudAssetSchema(); err != nil {
		return CloudAssetFeed{}, false, err
	}
	var f CloudAssetFeed
	err := s.db.QueryRow(
		`SELECT name, parent, feed_id, asset_types_json, content_type, pubsub_topic, body_json, created_at, updated_at
		 FROM cloudasset_feeds WHERE name = ?`,
		name,
	).Scan(&f.Name, &f.Parent, &f.FeedID, &f.AssetTypesJSON, &f.ContentType, &f.PubsubTopic, &f.BodyJSON, &f.CreatedAt, &f.UpdatedAt)
	if err == sql.ErrNoRows {
		return CloudAssetFeed{}, false, nil
	}
	if err != nil {
		return CloudAssetFeed{}, false, err
	}
	return f, true, nil
}

// ListCloudAssetFeeds lists feeds under parent (projects/... / folders/... / organizations/...).
func (s *Store) ListCloudAssetFeeds(parent string) ([]CloudAssetFeed, error) {
	if err := s.ensureCloudAssetSchema(); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(
		`SELECT name, parent, feed_id, asset_types_json, content_type, pubsub_topic, body_json, created_at, updated_at
		 FROM cloudasset_feeds WHERE parent = ? ORDER BY feed_id`,
		parent,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CloudAssetFeed
	for rows.Next() {
		var f CloudAssetFeed
		if err := rows.Scan(&f.Name, &f.Parent, &f.FeedID, &f.AssetTypesJSON, &f.ContentType, &f.PubsubTopic, &f.BodyJSON, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// DeleteCloudAssetFeed removes a feed by name. Returns false when missing.
func (s *Store) DeleteCloudAssetFeed(name string) (bool, error) {
	if err := s.ensureCloudAssetSchema(); err != nil {
		return false, err
	}
	res, err := s.db.Exec(`DELETE FROM cloudasset_feeds WHERE name = ?`, name)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// InsertCloudAssetHistory appends a TemporalAsset theatre row.
func (s *Store) InsertCloudAssetHistory(row CloudAssetHistoryRow) error {
	if err := s.ensureCloudAssetSchema(); err != nil {
		return err
	}
	if row.Parent == "" || row.AssetName == "" {
		return fmt.Errorf("parent and asset name required")
	}
	if row.ContentJSON == "" {
		row.ContentJSON = "{}"
	}
	if row.CreatedAt == "" {
		row.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	_, err := s.db.Exec(
		`INSERT INTO cloudasset_asset_history
		 (parent, asset_name, asset_type, content_json, window_start, window_end, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		row.Parent, row.AssetName, row.AssetType, row.ContentJSON, row.WindowStart, row.WindowEnd, row.CreatedAt,
	)
	return err
}

// ListCloudAssetHistory returns history rows for parent, optionally filtered by asset names.
func (s *Store) ListCloudAssetHistory(parent string, assetNames []string) ([]CloudAssetHistoryRow, error) {
	if err := s.ensureCloudAssetSchema(); err != nil {
		return nil, err
	}
	parent = strings.TrimSpace(parent)
	if parent == "" {
		return nil, fmt.Errorf("parent required")
	}
	var (
		rows *sql.Rows
		err  error
	)
	if len(assetNames) == 0 {
		rows, err = s.db.Query(
			`SELECT id, parent, asset_name, asset_type, content_json, window_start, window_end, created_at
			 FROM cloudasset_asset_history WHERE parent = ? ORDER BY id`,
			parent,
		)
	} else {
		placeholders := make([]string, len(assetNames))
		args := make([]any, 0, 1+len(assetNames))
		args = append(args, parent)
		for i, n := range assetNames {
			placeholders[i] = "?"
			args = append(args, n)
		}
		q := `SELECT id, parent, asset_name, asset_type, content_json, window_start, window_end, created_at
		 FROM cloudasset_asset_history WHERE parent = ? AND asset_name IN (` + strings.Join(placeholders, ",") + `) ORDER BY id`
		rows, err = s.db.Query(q, args...)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CloudAssetHistoryRow
	for rows.Next() {
		var r CloudAssetHistoryRow
		if err := rows.Scan(&r.ID, &r.Parent, &r.AssetName, &r.AssetType, &r.ContentJSON, &r.WindowStart, &r.WindowEnd, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
