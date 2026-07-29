package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// GCS notification event types (JSON API).
const (
	GCSEventObjectFinalize = "OBJECT_FINALIZE"
	GCSEventObjectDelete   = "OBJECT_DELETE"
)

// NotificationConfig is a Cloud Storage Pub/Sub notification configuration.
type NotificationConfig struct {
	Bucket           string
	ID               string
	Topic            string
	EventTypes       []string
	CustomAttributes map[string]string
	PayloadFormat    string
	ObjectNamePrefix string
	Etag             string
	CreatedAt        string
}

// CreateNotificationConfig inserts a notification config for a bucket.
func (s *Store) CreateNotificationConfig(bucket string, n NotificationConfig) (*NotificationConfig, error) {
	bucket = strings.TrimSpace(bucket)
	if bucket == "" {
		return nil, fmt.Errorf("bucket required")
	}
	b, ok, err := s.GetBucket(bucket)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("bucket not found")
	}
	topic := strings.TrimSpace(n.Topic)
	if topic == "" {
		return nil, fmt.Errorf("topic is required")
	}
	payloadFormat := strings.TrimSpace(n.PayloadFormat)
	if payloadFormat == "" {
		payloadFormat = "JSON_API_V1"
	}
	if payloadFormat != "JSON_API_V1" && payloadFormat != "NONE" {
		return nil, fmt.Errorf("invalid payload_format")
	}
	attrs := n.CustomAttributes
	if attrs == nil {
		attrs = map[string]string{}
	}
	if len(attrs) > 10 {
		return nil, fmt.Errorf("custom_attributes max 10")
	}
	eventTypes := n.EventTypes
	if eventTypes == nil {
		eventTypes = []string{}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	id, err := s.nextNotificationID(bucket)
	if err != nil {
		return nil, err
	}
	etag := fmt.Sprintf("CKxE%d", time.Now().UnixNano())
	_, err = s.db.Exec(
		`INSERT INTO gcs_notification_configs
		 (bucket, id, topic, event_types_json, custom_attributes_json, payload_format, object_name_prefix, etag, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		bucket, id, topic, encodeStringSlice(eventTypes), encodeStringMap(attrs),
		payloadFormat, n.ObjectNamePrefix, etag, now,
	)
	if err != nil {
		return nil, err
	}
	if err := s.bumpBucketMetageneration(bucket, b.Metageneration); err != nil {
		return nil, err
	}
	return &NotificationConfig{
		Bucket: bucket, ID: id, Topic: topic, EventTypes: eventTypes,
		CustomAttributes: attrs, PayloadFormat: payloadFormat,
		ObjectNamePrefix: n.ObjectNamePrefix, Etag: etag, CreatedAt: now,
	}, nil
}

func (s *Store) nextNotificationID(bucket string) (string, error) {
	var maxID int64
	err := s.db.QueryRow(
		`SELECT COALESCE(MAX(CAST(id AS INTEGER)), 0) FROM gcs_notification_configs WHERE bucket = ?`,
		bucket,
	).Scan(&maxID)
	if err != nil {
		return "", err
	}
	return strconv.FormatInt(maxID+1, 10), nil
}

func (s *Store) bumpBucketMetageneration(bucket string, current int64) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.Exec(
		`UPDATE buckets SET metageneration = ?, updated_at = ? WHERE name = ?`,
		current+1, now, bucket,
	)
	return err
}

// GetNotificationConfig loads one notification config.
func (s *Store) GetNotificationConfig(bucket, id string) (*NotificationConfig, bool, error) {
	var n NotificationConfig
	var eventTypesJSON, attrsJSON string
	err := s.db.QueryRow(
		`SELECT bucket, id, topic, event_types_json, custom_attributes_json, payload_format,
		        object_name_prefix, etag, created_at
		 FROM gcs_notification_configs WHERE bucket = ? AND id = ?`,
		bucket, id,
	).Scan(
		&n.Bucket, &n.ID, &n.Topic, &eventTypesJSON, &attrsJSON, &n.PayloadFormat,
		&n.ObjectNamePrefix, &n.Etag, &n.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	n.EventTypes = decodeStringSlice(eventTypesJSON)
	n.CustomAttributes = decodeStringMap(attrsJSON)
	return &n, true, nil
}

// ListNotificationConfigs lists notification configs for a bucket.
func (s *Store) ListNotificationConfigs(bucket string) ([]NotificationConfig, error) {
	rows, err := s.db.Query(
		`SELECT bucket, id, topic, event_types_json, custom_attributes_json, payload_format,
		        object_name_prefix, etag, created_at
		 FROM gcs_notification_configs WHERE bucket = ? ORDER BY CAST(id AS INTEGER)`,
		bucket,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []NotificationConfig
	for rows.Next() {
		var n NotificationConfig
		var eventTypesJSON, attrsJSON string
		if err := rows.Scan(
			&n.Bucket, &n.ID, &n.Topic, &eventTypesJSON, &attrsJSON, &n.PayloadFormat,
			&n.ObjectNamePrefix, &n.Etag, &n.CreatedAt,
		); err != nil {
			return nil, err
		}
		n.EventTypes = decodeStringSlice(eventTypesJSON)
		n.CustomAttributes = decodeStringMap(attrsJSON)
		out = append(out, n)
	}
	return out, rows.Err()
}

// DeleteNotificationConfig removes a notification config.
func (s *Store) DeleteNotificationConfig(bucket, id string) (bool, error) {
	b, ok, err := s.GetBucket(bucket)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	res, err := s.db.Exec(`DELETE FROM gcs_notification_configs WHERE bucket = ? AND id = ?`, bucket, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 0 {
		return false, nil
	}
	if err := s.bumpBucketMetageneration(bucket, b.Metageneration); err != nil {
		return false, err
	}
	return true, nil
}

// NormalizePubSubNotificationTopic maps GCS notification topic URIs to lab topic names.
// Accepts //pubsub.googleapis.com/projects/{p}/topics/{t} or projects/{p}/topics/{t}.
func NormalizePubSubNotificationTopic(topic string) (string, error) {
	topic = strings.TrimSpace(topic)
	const prefix = "//pubsub.googleapis.com/"
	if strings.HasPrefix(topic, prefix) {
		topic = strings.TrimPrefix(topic, prefix)
	}
	if !strings.HasPrefix(topic, "projects/") || !strings.Contains(topic, "/topics/") {
		return "", fmt.Errorf("invalid topic format")
	}
	return topic, nil
}

// DeliverGCSNotifications publishes matching bucket notificationConfigs to Pub/Sub.
// Best-effort: missing topics and publish errors are skipped (no GCS SA publisher IAM check —
// lab theatre; Publish itself is fail-closed only when called via Pub/Sub API authz).
func (s *Store) DeliverGCSNotifications(eventType string, obj *ObjectMeta) {
	if obj == nil {
		return
	}
	cfgs, err := s.ListNotificationConfigs(obj.Bucket)
	if err != nil {
		return
	}
	eventTime := obj.UpdatedAt
	if eventTime == "" {
		eventTime = obj.CreatedAt
	}
	if eventTime == "" {
		eventTime = time.Now().UTC().Format(time.RFC3339Nano)
	}
	var bucketProject string
	if b, ok, err := s.GetBucket(obj.Bucket); err == nil && ok {
		bucketProject = b.ProjectID
	}
	for i := range cfgs {
		cfg := cfgs[i]
		if !notificationMatches(&cfg, eventType, obj.Name) {
			continue
		}
		topicName, err := NormalizePubSubNotificationTopic(cfg.Topic)
		if err != nil {
			continue
		}
		if bucketProject != "" {
			if topic, ok, err := s.GetTopic(topicName); err == nil && ok {
				if err := s.VPCSCDenyCrossPerimeter(bucketProject, topic.ProjectID, "pubsub.googleapis.com"); err != nil {
					continue
				}
			}
		}
		attrs := map[string]string{
			"notificationConfig": "projects/_/buckets/" + obj.Bucket + "/notificationConfigs/" + cfg.ID,
			"eventType":          eventType,
			"payloadFormat":      cfg.PayloadFormat,
			"bucketId":           obj.Bucket,
			"objectId":           obj.Name,
			"objectGeneration":   strconv.FormatInt(obj.Generation, 10),
			"eventTime":          eventTime,
		}
		for k, v := range cfg.CustomAttributes {
			attrs[k] = v
		}
		var data []byte
		if cfg.PayloadFormat != "NONE" {
			payload := objectNotificationPayload(obj, eventType)
			raw, err := json.Marshal(payload)
			if err != nil {
				continue
			}
			data = raw
		}
		_, _ = s.Publish(topicName, data, attrs)
	}
}

func notificationMatches(cfg *NotificationConfig, eventType, objectName string) bool {
	if cfg.ObjectNamePrefix != "" && !strings.HasPrefix(objectName, cfg.ObjectNamePrefix) {
		return false
	}
	if len(cfg.EventTypes) == 0 {
		return true
	}
	for _, et := range cfg.EventTypes {
		if et == eventType {
			return true
		}
	}
	return false
}

func objectNotificationPayload(o *ObjectMeta, eventType string) map[string]any {
	meta := o.Metadata
	if meta == nil {
		meta = map[string]string{}
	}
	out := map[string]any{
		"kind":           "storage#object",
		"id":             o.Bucket + "/" + o.Name + "/" + strconv.FormatInt(o.Generation, 10),
		"name":           o.Name,
		"bucket":         o.Bucket,
		"generation":     strconv.FormatInt(o.Generation, 10),
		"metageneration": strconv.FormatInt(o.Metageneration, 10),
		"size":           strconv.FormatInt(o.Size, 10),
		"contentType":    o.ContentType,
		"md5Hash":        o.MD5Hash,
		"crc32c":         o.CRC32C,
		"metadata":       meta,
		"timeCreated":    o.CreatedAt,
		"updated":        o.UpdatedAt,
		"storageClass":   "STANDARD",
	}
	if eventType == GCSEventObjectDelete {
		out["timeDeleted"] = time.Now().UTC().Format(time.RFC3339Nano)
	}
	return out
}

func encodeStringSlice(ss []string) string {
	if ss == nil {
		return "[]"
	}
	raw, err := json.Marshal(ss)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func decodeStringSlice(raw string) []string {
	if raw == "" || raw == "[]" {
		return []string{}
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out == nil {
		return []string{}
	}
	return out
}
