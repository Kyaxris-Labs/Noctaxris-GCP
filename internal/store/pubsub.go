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
	Labels    map[string]string
	CreatedAt string
}

// PubSubSubscription is a Pub/Sub subscription row.
type PubSubSubscription struct {
	Name                        string
	Topic                       string
	ProjectID                   string
	AckDeadlineSeconds          int
	PushEndpoint                string
	Labels                      map[string]string
	Filter                      string
	DeadLetterTopic             string
	MaxDeliveryAttempts         int
	EnableExactlyOnceDelivery   bool
	CreatedAt                   string
}

// PubSubMessage is a queued message for a subscription.
type PubSubMessage struct {
	ID               string
	Subscription     string
	Topic            string
	Data             []byte
	AttributesJSON   string
	PublishTime      string
	AckID            string
	AckDeadline      string
	Delivered        bool
	DeliveryAttempts int
}

// CreateTopic inserts a topic. created=false means already exists.
func (s *Store) CreateTopic(name, projectID string) (*PubSubTopic, bool, error) {
	return s.CreateTopicWithLabels(name, projectID, nil)
}

// CreateTopicWithLabels inserts a topic with optional labels.
func (s *Store) CreateTopicWithLabels(name, projectID string, labels map[string]string) (*PubSubTopic, bool, error) {
	name = strings.TrimSpace(name)
	projectID = strings.TrimSpace(projectID)
	if name == "" || projectID == "" {
		return nil, false, fmt.Errorf("topic name and project required")
	}
	if labels == nil {
		labels = map[string]string{}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO pubsub_topics (name, project_id, labels_json, created_at) VALUES (?, ?, ?, ?)`,
		name, projectID, encodeStringMap(labels), now,
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
	return &PubSubTopic{Name: name, ProjectID: projectID, Labels: labels, CreatedAt: now}, true, nil
}

// GetTopic loads a topic by resource name.
func (s *Store) GetTopic(name string) (*PubSubTopic, bool, error) {
	var t PubSubTopic
	var labelsJSON string
	err := s.db.QueryRow(
		`SELECT name, project_id, COALESCE(labels_json, '{}'), created_at FROM pubsub_topics WHERE name = ?`, name,
	).Scan(&t.Name, &t.ProjectID, &labelsJSON, &t.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	t.Labels = decodeStringMap(labelsJSON)
	return &t, true, nil
}

// UpdateTopicLabels replaces topic labels.
func (s *Store) UpdateTopicLabels(name string, labels map[string]string) (*PubSubTopic, error) {
	t, ok, err := s.GetTopic(name)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("topic not found")
	}
	if labels == nil {
		labels = map[string]string{}
	}
	_, err = s.db.Exec(`UPDATE pubsub_topics SET labels_json = ? WHERE name = ?`, encodeStringMap(labels), name)
	if err != nil {
		return nil, err
	}
	t.Labels = labels
	return t, nil
}

// ListTopics lists topics for a project id (not the projects/ prefix).
func (s *Store) ListTopics(projectID string) ([]PubSubTopic, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, COALESCE(labels_json, '{}'), created_at FROM pubsub_topics WHERE project_id = ? ORDER BY name`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PubSubTopic
	for rows.Next() {
		var t PubSubTopic
		var labelsJSON string
		if err := rows.Scan(&t.Name, &t.ProjectID, &labelsJSON, &t.CreatedAt); err != nil {
			return nil, err
		}
		t.Labels = decodeStringMap(labelsJSON)
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
	return s.CreateSubscriptionFull(name, topic, projectID, ackDeadlineSeconds, "", nil, "", "", 0, false)
}

// CreateSubscriptionFull inserts a subscription with push, labels, filter, dead-letter, and exactly-once theatre fields.
func (s *Store) CreateSubscriptionFull(name, topic, projectID string, ackDeadlineSeconds int, pushEndpoint string, labels map[string]string, filter, deadLetterTopic string, maxDeliveryAttempts int, enableExactlyOnce bool) (*PubSubSubscription, bool, error) {
	name = strings.TrimSpace(name)
	topic = strings.TrimSpace(topic)
	projectID = strings.TrimSpace(projectID)
	if name == "" || topic == "" || projectID == "" {
		return nil, false, fmt.Errorf("subscription name, topic, and project required")
	}
	if ackDeadlineSeconds <= 0 {
		ackDeadlineSeconds = 10
	}
	if labels == nil {
		labels = map[string]string{}
	}
	filter = strings.TrimSpace(filter)
	if filter != "" {
		if _, err := parseAttributeEqualityFilter(filter); err != nil {
			return nil, false, fmt.Errorf("invalid filter: %w", err)
		}
	}
	deadLetterTopic = strings.TrimSpace(deadLetterTopic)
	if deadLetterTopic != "" {
		if _, ok, err := s.GetTopic(deadLetterTopic); err != nil {
			return nil, false, err
		} else if !ok {
			return nil, false, fmt.Errorf("dead letter topic not found")
		}
		if maxDeliveryAttempts == 0 {
			maxDeliveryAttempts = 5
		}
		if maxDeliveryAttempts < 5 || maxDeliveryAttempts > 100 {
			return nil, false, fmt.Errorf("maxDeliveryAttempts must be between 5 and 100")
		}
	} else {
		maxDeliveryAttempts = 0
	}
	if _, ok, err := s.GetTopic(topic); err != nil {
		return nil, false, err
	} else if !ok {
		return nil, false, fmt.Errorf("topic not found")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	eos := 0
	if enableExactlyOnce {
		eos = 1
	}
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO pubsub_subscriptions
		 (name, topic, project_id, ack_deadline_seconds, push_endpoint, labels_json, filter,
		  dead_letter_topic, max_delivery_attempts, enable_exactly_once_delivery, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		name, topic, projectID, ackDeadlineSeconds, strings.TrimSpace(pushEndpoint), encodeStringMap(labels), filter,
		deadLetterTopic, maxDeliveryAttempts, eos, now,
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
		AckDeadlineSeconds: ackDeadlineSeconds, PushEndpoint: strings.TrimSpace(pushEndpoint),
		Labels: labels, Filter: filter, DeadLetterTopic: deadLetterTopic,
		MaxDeliveryAttempts: maxDeliveryAttempts, EnableExactlyOnceDelivery: enableExactlyOnce,
		CreatedAt: now,
	}, true, nil
}

// GetSubscription loads a subscription by resource name.
func (s *Store) GetSubscription(name string) (*PubSubSubscription, bool, error) {
	var sub PubSubSubscription
	var labelsJSON string
	var eos int
	err := s.db.QueryRow(
		`SELECT name, topic, project_id, ack_deadline_seconds, COALESCE(push_endpoint, ''), COALESCE(labels_json, '{}'),
		        COALESCE(filter, ''), COALESCE(dead_letter_topic, ''), COALESCE(max_delivery_attempts, 0),
		        COALESCE(enable_exactly_once_delivery, 0), created_at
		 FROM pubsub_subscriptions WHERE name = ?`,
		name,
	).Scan(&sub.Name, &sub.Topic, &sub.ProjectID, &sub.AckDeadlineSeconds, &sub.PushEndpoint, &labelsJSON,
		&sub.Filter, &sub.DeadLetterTopic, &sub.MaxDeliveryAttempts, &eos, &sub.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	sub.Labels = decodeStringMap(labelsJSON)
	sub.EnableExactlyOnceDelivery = eos != 0
	return &sub, true, nil
}

// PubSubDeadLetterPolicy is the stored dead-letter configuration for a subscription.
type PubSubDeadLetterPolicy struct {
	DeadLetterTopic     string
	MaxDeliveryAttempts int
}

// UpdateSubscription applies mutable subscription fields. Nil pointers leave fields unchanged.
func (s *Store) UpdateSubscription(name string, ackDeadline *int, pushEndpoint *string, labels *map[string]string, filter *string, deadLetter *PubSubDeadLetterPolicy, enableExactlyOnce *bool) (*PubSubSubscription, error) {
	sub, ok, err := s.GetSubscription(name)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("subscription not found")
	}
	if ackDeadline != nil {
		v := *ackDeadline
		if v <= 0 {
			v = 10
		}
		sub.AckDeadlineSeconds = v
	}
	if pushEndpoint != nil {
		sub.PushEndpoint = strings.TrimSpace(*pushEndpoint)
	}
	if labels != nil {
		sub.Labels = *labels
		if sub.Labels == nil {
			sub.Labels = map[string]string{}
		}
	}
	if filter != nil {
		f := strings.TrimSpace(*filter)
		if f != "" {
			if _, err := parseAttributeEqualityFilter(f); err != nil {
				return nil, fmt.Errorf("invalid filter: %w", err)
			}
		}
		sub.Filter = f
	}
	if deadLetter != nil {
		topic := strings.TrimSpace(deadLetter.DeadLetterTopic)
		if topic == "" {
			sub.DeadLetterTopic = ""
			sub.MaxDeliveryAttempts = 0
		} else {
			if _, ok, err := s.GetTopic(topic); err != nil {
				return nil, err
			} else if !ok {
				return nil, fmt.Errorf("dead letter topic not found")
			}
			attempts := deadLetter.MaxDeliveryAttempts
			if attempts == 0 {
				attempts = 5
			}
			if attempts < 5 || attempts > 100 {
				return nil, fmt.Errorf("maxDeliveryAttempts must be between 5 and 100")
			}
			sub.DeadLetterTopic = topic
			sub.MaxDeliveryAttempts = attempts
		}
	}
	if enableExactlyOnce != nil {
		sub.EnableExactlyOnceDelivery = *enableExactlyOnce
	}
	eos := 0
	if sub.EnableExactlyOnceDelivery {
		eos = 1
	}
	_, err = s.db.Exec(
		`UPDATE pubsub_subscriptions SET ack_deadline_seconds = ?, push_endpoint = ?, labels_json = ?, filter = ?,
		 dead_letter_topic = ?, max_delivery_attempts = ?, enable_exactly_once_delivery = ? WHERE name = ?`,
		sub.AckDeadlineSeconds, sub.PushEndpoint, encodeStringMap(sub.Labels), sub.Filter,
		sub.DeadLetterTopic, sub.MaxDeliveryAttempts, eos, name,
	)
	if err != nil {
		return nil, err
	}
	return sub, nil
}

// ListSubscriptions lists subscriptions for a project id.
func (s *Store) ListSubscriptions(projectID string) ([]PubSubSubscription, error) {
	rows, err := s.db.Query(
		`SELECT name, topic, project_id, ack_deadline_seconds, COALESCE(push_endpoint, ''), COALESCE(labels_json, '{}'),
		        COALESCE(filter, ''), COALESCE(dead_letter_topic, ''), COALESCE(max_delivery_attempts, 0),
		        COALESCE(enable_exactly_once_delivery, 0), created_at
		 FROM pubsub_subscriptions WHERE project_id = ? ORDER BY name`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PubSubSubscription
	for rows.Next() {
		var sub PubSubSubscription
		var labelsJSON string
		var eos int
		if err := rows.Scan(&sub.Name, &sub.Topic, &sub.ProjectID, &sub.AckDeadlineSeconds, &sub.PushEndpoint, &labelsJSON,
			&sub.Filter, &sub.DeadLetterTopic, &sub.MaxDeliveryAttempts, &eos, &sub.CreatedAt); err != nil {
			return nil, err
		}
		sub.Labels = decodeStringMap(labelsJSON)
		sub.EnableExactlyOnceDelivery = eos != 0
		out = append(out, sub)
	}
	return out, rows.Err()
}

// ListSubscriptionsForTopic lists all subscriptions attached to a topic.
func (s *Store) ListSubscriptionsForTopic(topic string) ([]PubSubSubscription, error) {
	rows, err := s.db.Query(
		`SELECT name, topic, project_id, ack_deadline_seconds, COALESCE(push_endpoint, ''), COALESCE(labels_json, '{}'),
		        COALESCE(filter, ''), COALESCE(dead_letter_topic, ''), COALESCE(max_delivery_attempts, 0),
		        COALESCE(enable_exactly_once_delivery, 0), created_at
		 FROM pubsub_subscriptions WHERE topic = ? ORDER BY name`,
		topic,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PubSubSubscription
	for rows.Next() {
		var sub PubSubSubscription
		var labelsJSON string
		var eos int
		if err := rows.Scan(&sub.Name, &sub.Topic, &sub.ProjectID, &sub.AckDeadlineSeconds, &sub.PushEndpoint, &labelsJSON,
			&sub.Filter, &sub.DeadLetterTopic, &sub.MaxDeliveryAttempts, &eos, &sub.CreatedAt); err != nil {
			return nil, err
		}
		sub.Labels = decodeStringMap(labelsJSON)
		sub.EnableExactlyOnceDelivery = eos != 0
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

// PublishFanout fans out one message to every subscription of the topic.
func (s *Store) PublishFanout(topic string, data []byte, attributes map[string]string) (messageID string, copies []PubSubMessage, err error) {
	if _, ok, err := s.GetTopic(topic); err != nil {
		return "", nil, err
	} else if !ok {
		return "", nil, fmt.Errorf("topic not found")
	}
	attrsJSON := "{}"
	if attributes != nil {
		raw, err := json.Marshal(attributes)
		if err != nil {
			return "", nil, err
		}
		attrsJSON = string(raw)
	}
	subs, err := s.ListSubscriptionsForTopic(topic)
	if err != nil {
		return "", nil, err
	}
	messageID = uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.Begin()
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = tx.Rollback() }()
	for _, sub := range subs {
		if !messageMatchesFilter(sub.Filter, attributes) {
			continue
		}
		ackID := uuid.NewString()
		if _, err := tx.Exec(
			`INSERT INTO pubsub_messages (id, subscription, topic, data, attributes_json, publish_time, ack_id, ack_deadline, delivered)
			 VALUES (?, ?, ?, ?, ?, ?, ?, NULL, 0)`,
			messageID, sub.Name, topic, data, attrsJSON, now, ackID,
		); err != nil {
			return "", nil, err
		}
		copies = append(copies, PubSubMessage{
			ID: messageID, Subscription: sub.Name, Topic: topic, Data: data,
			AttributesJSON: attrsJSON, PublishTime: now, AckID: ackID,
		})
	}
	if err := tx.Commit(); err != nil {
		return "", nil, err
	}
	// Best-effort Eventarc delivery for Pub/Sub messagePublished triggers.
	go s.DeliverEventarcForPubSub(topic, data, attributes)
	return messageID, copies, nil
}

// Publish fans out one message to every subscription of the topic.
// Returns the shared message id used for each fan-out copy.
func (s *Store) Publish(topic string, data []byte, attributes map[string]string) (messageID string, err error) {
	id, _, err := s.PublishFanout(topic, data, attributes)
	return id, err
}

// Pull returns up to maxMessages undelivered-or-expired messages and leases them.
// When a dead-letter policy is set and delivery attempts reach the max, the message
// is published to the dead-letter topic and removed from this subscription.
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
	// Over-fetch so dead-lettered messages do not starve the pull window.
	fetchN := maxMessages * 4
	if fetchN < maxMessages {
		fetchN = maxMessages
	}
	rows, err := s.db.Query(
		`SELECT id, subscription, topic, data, attributes_json, publish_time, ack_id, COALESCE(ack_deadline, ''), delivered,
		        COALESCE(delivery_attempts, 0)
		 FROM pubsub_messages
		 WHERE subscription = ?
		   AND (delivered = 0 OR ack_deadline IS NULL OR ack_deadline < ?)
		 ORDER BY publish_time
		 LIMIT ?`,
		subscription, nowStr, fetchN,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var candidates []PubSubMessage
	for rows.Next() {
		var m PubSubMessage
		var delivered int
		if err := rows.Scan(&m.ID, &m.Subscription, &m.Topic, &m.Data, &m.AttributesJSON, &m.PublishTime, &m.AckID, &m.AckDeadline, &delivered, &m.DeliveryAttempts); err != nil {
			return nil, err
		}
		m.Delivered = delivered != 0
		candidates = append(candidates, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	deadline := now.Add(time.Duration(sub.AckDeadlineSeconds) * time.Second).UTC().Format(time.RFC3339Nano)
	var msgs []PubSubMessage
	for i := range candidates {
		if len(msgs) >= maxMessages {
			break
		}
		m := candidates[i]
		attempts := m.DeliveryAttempts + 1
		if sub.DeadLetterTopic != "" && sub.MaxDeliveryAttempts > 0 && attempts >= sub.MaxDeliveryAttempts {
			attrs := map[string]string{}
			if m.AttributesJSON != "" && m.AttributesJSON != "{}" {
				_ = json.Unmarshal([]byte(m.AttributesJSON), &attrs)
			}
			attrs["CloudPubSubDeadLetterSourceSubscription"] = subscription
			if _, err := s.Publish(sub.DeadLetterTopic, m.Data, attrs); err != nil {
				return nil, fmt.Errorf("dead letter publish: %w", err)
			}
			if _, err := s.db.Exec(`DELETE FROM pubsub_messages WHERE ack_id = ?`, m.AckID); err != nil {
				return nil, err
			}
			continue
		}
		if _, err := s.db.Exec(
			`UPDATE pubsub_messages SET delivered = 1, ack_deadline = ?, delivery_attempts = ? WHERE ack_id = ?`,
			deadline, attempts, m.AckID,
		); err != nil {
			return nil, err
		}
		m.Delivered = true
		m.AckDeadline = deadline
		m.DeliveryAttempts = attempts
		msgs = append(msgs, m)
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

// ModifyAckDeadline updates the lease deadline for the given ack ids.
func (s *Store) ModifyAckDeadline(subscription string, ackIDs []string, ackDeadlineSeconds int) error {
	if len(ackIDs) == 0 {
		return nil
	}
	if ackDeadlineSeconds < 0 {
		ackDeadlineSeconds = 0
	}
	if ackDeadlineSeconds > 600 {
		ackDeadlineSeconds = 600
	}
	deadline := time.Now().UTC().Add(time.Duration(ackDeadlineSeconds) * time.Second).Format(time.RFC3339Nano)
	if ackDeadlineSeconds == 0 {
		// Immediate redelivery (lease expired).
		deadline = time.Now().UTC().Add(-time.Second).Format(time.RFC3339Nano)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, id := range ackIDs {
		if _, err := tx.Exec(
			`UPDATE pubsub_messages SET delivered = 1, ack_deadline = ? WHERE subscription = ? AND ack_id = ?`,
			deadline, subscription, id,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SeekToTime seeks a subscription to time t:
// messages with publish_time < t are deleted (acked); later messages have ack state cleared.
func (s *Store) SeekToTime(subscription string, t time.Time) error {
	if _, ok, err := s.GetSubscription(subscription); err != nil {
		return err
	} else if !ok {
		return fmt.Errorf("subscription not found")
	}
	ts := t.UTC().Format(time.RFC3339Nano)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(
		`DELETE FROM pubsub_messages WHERE subscription = ? AND publish_time < ?`,
		subscription, ts,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`UPDATE pubsub_messages SET delivered = 0, ack_deadline = NULL WHERE subscription = ? AND publish_time >= ?`,
		subscription, ts,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// parseAttributeEqualityFilter parses lab filters of the form:
// attributes.key = "value" [AND attributes.other = "x"]
func parseAttributeEqualityFilter(filter string) (map[string]string, error) {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return map[string]string{}, nil
	}
	parts := strings.Split(filter, " AND ")
	out := map[string]string{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		const prefix = "attributes."
		if !strings.HasPrefix(part, prefix) {
			return nil, fmt.Errorf("only attributes.<key> = \"value\" equality is supported")
		}
		rest := strings.TrimSpace(part[len(prefix):])
		eq := strings.Index(rest, "=")
		if eq < 0 {
			return nil, fmt.Errorf("missing = in filter term")
		}
		key := strings.TrimSpace(rest[:eq])
		valRaw := strings.TrimSpace(rest[eq+1:])
		if key == "" {
			return nil, fmt.Errorf("empty attribute key")
		}
		if len(valRaw) < 2 || valRaw[0] != '"' || valRaw[len(valRaw)-1] != '"' {
			return nil, fmt.Errorf("filter value must be a quoted string")
		}
		out[key] = valRaw[1 : len(valRaw)-1]
	}
	return out, nil
}

func messageMatchesFilter(filter string, attributes map[string]string) bool {
	want, err := parseAttributeEqualityFilter(filter)
	if err != nil || len(want) == 0 {
		return err == nil
	}
	if attributes == nil {
		attributes = map[string]string{}
	}
	for k, v := range want {
		if attributes[k] != v {
			return false
		}
	}
	return true
}

// PubSubSnapshot is a Pub/Sub snapshot metadata row (lite; no backlog copy).
type PubSubSnapshot struct {
	Name         string
	ProjectID    string
	Topic        string
	Subscription string
	Labels       map[string]string
	ExpireTime   string
	CreatedAt    string
}

// CreateSnapshot inserts a snapshot from a subscription. created=false means already exists.
func (s *Store) CreateSnapshot(name, subscription string, labels map[string]string) (*PubSubSnapshot, bool, error) {
	name = strings.TrimSpace(name)
	subscription = strings.TrimSpace(subscription)
	if name == "" || subscription == "" {
		return nil, false, fmt.Errorf("snapshot name and subscription required")
	}
	sub, ok, err := s.GetSubscription(subscription)
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, false, fmt.Errorf("subscription not found")
	}
	parts := strings.Split(name, "/")
	if len(parts) < 4 || parts[0] != "projects" || parts[2] != "snapshots" {
		return nil, false, fmt.Errorf("invalid snapshot name")
	}
	projectID := parts[1]
	if labels == nil {
		labels = map[string]string{}
	}
	now := time.Now().UTC()
	expire := now.Add(7 * 24 * time.Hour).Format(time.RFC3339Nano)
	created := now.Format(time.RFC3339Nano)
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO pubsub_snapshots (name, project_id, topic, subscription, labels_json, expire_time, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		name, projectID, sub.Topic, subscription, encodeStringMap(labels), expire, created,
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
	return &PubSubSnapshot{
		Name: name, ProjectID: projectID, Topic: sub.Topic, Subscription: subscription,
		Labels: labels, ExpireTime: expire, CreatedAt: created,
	}, true, nil
}

// GetSnapshot loads a snapshot by resource name.
func (s *Store) GetSnapshot(name string) (*PubSubSnapshot, bool, error) {
	var snap PubSubSnapshot
	var labelsJSON string
	err := s.db.QueryRow(
		`SELECT name, project_id, topic, subscription, COALESCE(labels_json, '{}'), expire_time, created_at
		 FROM pubsub_snapshots WHERE name = ?`, name,
	).Scan(&snap.Name, &snap.ProjectID, &snap.Topic, &snap.Subscription, &labelsJSON, &snap.ExpireTime, &snap.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	snap.Labels = decodeStringMap(labelsJSON)
	return &snap, true, nil
}

// ListSnapshots lists snapshots for a project id (not the projects/ prefix).
func (s *Store) ListSnapshots(projectID string) ([]PubSubSnapshot, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, topic, subscription, COALESCE(labels_json, '{}'), expire_time, created_at
		 FROM pubsub_snapshots WHERE project_id = ? ORDER BY name`,
		projectID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PubSubSnapshot
	for rows.Next() {
		var snap PubSubSnapshot
		var labelsJSON string
		if err := rows.Scan(&snap.Name, &snap.ProjectID, &snap.Topic, &snap.Subscription, &labelsJSON, &snap.ExpireTime, &snap.CreatedAt); err != nil {
			return nil, err
		}
		snap.Labels = decodeStringMap(labelsJSON)
		out = append(out, snap)
	}
	return out, rows.Err()
}

// DeleteSnapshot removes a snapshot by resource name.
func (s *Store) DeleteSnapshot(name string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM pubsub_snapshots WHERE name = ?`, name)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
