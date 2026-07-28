package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// PubSubTopic is a Pub/Sub topic row.
type PubSubTopic struct {
	Name      string
	ProjectID string
	CreatedAt string
}

// PubSubSubscription is a Pub/Sub subscription row.
type PubSubSubscription struct {
	Name               string
	Topic              string
	ProjectID          string
	AckDeadlineSeconds int
	CreatedAt          string
}

// PubSubMessage is a queued message for a subscription.
type PubSubMessage struct {
	ID             string
	Subscription   string
	Topic          string
	Data           []byte
	AttributesJSON string
	PublishTime    string
	AckID          string
	AckDeadline    string
	Delivered      bool
}

// CreateTopic inserts a topic. created=false means already exists.
func (s *Store) CreateTopic(name, projectID string) (*PubSubTopic, bool, error) {
	name = strings.TrimSpace(name)
	projectID = strings.TrimSpace(projectID)
	if name == "" || projectID == "" {
		return nil, false, fmt.Errorf("topic name and project required")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO pubsub_topics (name, project_id, created_at) VALUES (?, ?, ?)`,
		name, projectID, now,
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
	return &PubSubTopic{Name: name, ProjectID: projectID, CreatedAt: now}, true, nil
}

// GetTopic loads a topic by resource name.
func (s *Store) GetTopic(name string) (*PubSubTopic, bool, error) {
	var t PubSubTopic
	err := s.db.QueryRow(
		`SELECT name, project_id, created_at FROM pubsub_topics WHERE name = ?`, name,
	).Scan(&t.Name, &t.ProjectID, &t.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &t, true, nil
}

// ListTopics lists topics for a project id (not the projects/ prefix).
func (s *Store) ListTopics(projectID string) ([]PubSubTopic, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, created_at FROM pubsub_topics WHERE project_id = ? ORDER BY name`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PubSubTopic
	for rows.Next() {
		var t PubSubTopic
		if err := rows.Scan(&t.Name, &t.ProjectID, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteTopic removes a topic (subscriptions remain but topic is gone).
func (s *Store) DeleteTopic(name string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM pubsub_topics WHERE name = ?`, name)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// CreateSubscription inserts a subscription. created=false means already exists.
func (s *Store) CreateSubscription(name, topic, projectID string, ackDeadlineSeconds int) (*PubSubSubscription, bool, error) {
	name = strings.TrimSpace(name)
	topic = strings.TrimSpace(topic)
	projectID = strings.TrimSpace(projectID)
	if name == "" || topic == "" || projectID == "" {
		return nil, false, fmt.Errorf("subscription name, topic, and project required")
	}
	if ackDeadlineSeconds <= 0 {
		ackDeadlineSeconds = 10
	}
	if _, ok, err := s.GetTopic(topic); err != nil {
		return nil, false, err
	} else if !ok {
		return nil, false, fmt.Errorf("topic not found")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO pubsub_subscriptions (name, topic, project_id, ack_deadline_seconds, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		name, topic, projectID, ackDeadlineSeconds, now,
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
	return &PubSubSubscription{
		Name: name, Topic: topic, ProjectID: projectID,
		AckDeadlineSeconds: ackDeadlineSeconds, CreatedAt: now,
	}, true, nil
}

// GetSubscription loads a subscription by resource name.
func (s *Store) GetSubscription(name string) (*PubSubSubscription, bool, error) {
	var sub PubSubSubscription
	err := s.db.QueryRow(
		`SELECT name, topic, project_id, ack_deadline_seconds, created_at FROM pubsub_subscriptions WHERE name = ?`,
		name,
	).Scan(&sub.Name, &sub.Topic, &sub.ProjectID, &sub.AckDeadlineSeconds, &sub.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &sub, true, nil
}

// ListSubscriptions lists subscriptions for a project id.
func (s *Store) ListSubscriptions(projectID string) ([]PubSubSubscription, error) {
	rows, err := s.db.Query(
		`SELECT name, topic, project_id, ack_deadline_seconds, created_at FROM pubsub_subscriptions WHERE project_id = ? ORDER BY name`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PubSubSubscription
	for rows.Next() {
		var sub PubSubSubscription
		if err := rows.Scan(&sub.Name, &sub.Topic, &sub.ProjectID, &sub.AckDeadlineSeconds, &sub.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

// DeleteSubscription removes a subscription and its messages.
func (s *Store) DeleteSubscription(name string) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM pubsub_messages WHERE subscription = ?`, name); err != nil {
		return false, err
	}
	res, err := tx.Exec(`DELETE FROM pubsub_subscriptions WHERE name = ?`, name)
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

// Publish fans out one message to every subscription of the topic.
// Returns the shared message id used for each fan-out copy.
func (s *Store) Publish(topic string, data []byte, attributes map[string]string) (messageID string, err error) {
	if _, ok, err := s.GetTopic(topic); err != nil {
		return "", err
	} else if !ok {
		return "", fmt.Errorf("topic not found")
	}
	attrsJSON := "{}"
	if attributes != nil {
		raw, err := json.Marshal(attributes)
		if err != nil {
			return "", err
		}
		attrsJSON = string(raw)
	}
	rows, err := s.db.Query(`SELECT name FROM pubsub_subscriptions WHERE topic = ?`, topic)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var subs []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return "", err
		}
		subs = append(subs, n)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	messageID = uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	for _, sub := range subs {
		ackID := uuid.NewString()
		if _, err := tx.Exec(
			`INSERT INTO pubsub_messages (id, subscription, topic, data, attributes_json, publish_time, ack_id, ack_deadline, delivered)
			 VALUES (?, ?, ?, ?, ?, ?, ?, NULL, 0)`,
			messageID, sub, topic, data, attrsJSON, now, ackID,
		); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return messageID, nil
}

// Pull returns up to maxMessages undelivered-or-expired messages and leases them.
func (s *Store) Pull(subscription string, maxMessages int) ([]PubSubMessage, error) {
	if maxMessages <= 0 {
		maxMessages = 1
	}
	sub, ok, err := s.GetSubscription(subscription)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("subscription not found")
	}
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339Nano)
	rows, err := s.db.Query(
		`SELECT id, subscription, topic, data, attributes_json, publish_time, ack_id, COALESCE(ack_deadline, ''), delivered
		 FROM pubsub_messages
		 WHERE subscription = ?
		   AND (delivered = 0 OR ack_deadline IS NULL OR ack_deadline < ?)
		 ORDER BY publish_time
		 LIMIT ?`,
		subscription, nowStr, maxMessages,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var msgs []PubSubMessage
	for rows.Next() {
		var m PubSubMessage
		var delivered int
		if err := rows.Scan(&m.ID, &m.Subscription, &m.Topic, &m.Data, &m.AttributesJSON, &m.PublishTime, &m.AckID, &m.AckDeadline, &delivered); err != nil {
			return nil, err
		}
		m.Delivered = delivered != 0
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	deadline := now.Add(time.Duration(sub.AckDeadlineSeconds) * time.Second).UTC().Format(time.RFC3339Nano)
	for i := range msgs {
		if _, err := s.db.Exec(
			`UPDATE pubsub_messages SET delivered = 1, ack_deadline = ? WHERE ack_id = ?`,
			deadline, msgs[i].AckID,
		); err != nil {
			return nil, err
		}
		msgs[i].Delivered = true
		msgs[i].AckDeadline = deadline
	}
	return msgs, nil
}

// Acknowledge deletes messages by ack id for a subscription.
func (s *Store) Acknowledge(subscription string, ackIDs []string) error {
	if len(ackIDs) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, id := range ackIDs {
		if _, err := tx.Exec(
			`DELETE FROM pubsub_messages WHERE subscription = ? AND ack_id = ?`,
			subscription, id,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}
