package store

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/httpegress"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/labtoken"
	"github.com/google/uuid"
)

// --- Eventarc ---

// EventarcTrigger is a trigger row.
type EventarcTrigger struct {
	Name            string
	ProjectID       string
	Location        string
	TriggerID       string
	FiltersJSON     string
	DestinationJSON string
	TransportJSON   string
	Channel         string
	ServiceAccount  string
	CreatedAt       string
}

// EventarcChannel is a channel stub row.
type EventarcChannel struct {
	Name        string
	ProjectID   string
	Location    string
	ChannelID   string
	UID         string
	Provider    string
	PubsubTopic string
	State       string
	CreatedAt   string
}

// CreateEventarcTrigger inserts a trigger. created=false means already exists.
func (s *Store) CreateEventarcTrigger(t EventarcTrigger) (*EventarcTrigger, bool, error) {
	t.ProjectID = strings.TrimSpace(t.ProjectID)
	t.Location = strings.TrimSpace(t.Location)
	t.TriggerID = strings.TrimSpace(t.TriggerID)
	if t.ProjectID == "" || t.Location == "" || t.TriggerID == "" {
		return nil, false, fmt.Errorf("project, location, and trigger id required")
	}
	if t.Name == "" {
		t.Name = "projects/" + t.ProjectID + "/locations/" + t.Location + "/triggers/" + t.TriggerID
	}
	if t.FiltersJSON == "" {
		t.FiltersJSON = "[]"
	}
	if t.DestinationJSON == "" {
		t.DestinationJSON = "{}"
	}
	if t.TransportJSON == "" {
		t.TransportJSON = "{}"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	t.CreatedAt = now
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO eventarc_triggers
		 (name, project_id, location, trigger_id, filters_json, destination_json, transport_json, channel, service_account, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.Name, t.ProjectID, t.Location, t.TriggerID, t.FiltersJSON, t.DestinationJSON, t.TransportJSON, t.Channel, t.ServiceAccount, now,
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
	return &t, true, nil
}

// GetEventarcTrigger loads by resource name.
func (s *Store) GetEventarcTrigger(name string) (*EventarcTrigger, bool, error) {
	var t EventarcTrigger
	err := s.db.QueryRow(
		`SELECT name, project_id, location, trigger_id, filters_json, destination_json, transport_json, channel, COALESCE(service_account, ''), created_at
		 FROM eventarc_triggers WHERE name = ?`,
		name,
	).Scan(&t.Name, &t.ProjectID, &t.Location, &t.TriggerID, &t.FiltersJSON, &t.DestinationJSON, &t.TransportJSON, &t.Channel, &t.ServiceAccount, &t.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &t, true, nil
}

// ListEventarcTriggers lists triggers for project+location ("-" lists all locations).
func (s *Store) ListEventarcTriggers(projectID, location string) ([]EventarcTrigger, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if location == "" || location == "-" {
		rows, err = s.db.Query(
			`SELECT name, project_id, location, trigger_id, filters_json, destination_json, transport_json, channel, COALESCE(service_account, ''), created_at
			 FROM eventarc_triggers WHERE project_id = ? ORDER BY name`,
			projectID,
		)
	} else {
		rows, err = s.db.Query(
			`SELECT name, project_id, location, trigger_id, filters_json, destination_json, transport_json, channel, COALESCE(service_account, ''), created_at
			 FROM eventarc_triggers WHERE project_id = ? AND location = ? ORDER BY name`,
			projectID, location,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EventarcTrigger
	for rows.Next() {
		var t EventarcTrigger
		if err := rows.Scan(&t.Name, &t.ProjectID, &t.Location, &t.TriggerID, &t.FiltersJSON, &t.DestinationJSON, &t.TransportJSON, &t.Channel, &t.ServiceAccount, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteEventarcTrigger removes a trigger.
func (s *Store) DeleteEventarcTrigger(name string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM eventarc_triggers WHERE name = ?`, name)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// CreateEventarcChannel inserts a channel stub. created=false means already exists.
func (s *Store) CreateEventarcChannel(c EventarcChannel) (*EventarcChannel, bool, error) {
	c.ProjectID = strings.TrimSpace(c.ProjectID)
	c.Location = strings.TrimSpace(c.Location)
	c.ChannelID = strings.TrimSpace(c.ChannelID)
	if c.ProjectID == "" || c.Location == "" || c.ChannelID == "" {
		return nil, false, fmt.Errorf("project, location, and channel id required")
	}
	if c.Name == "" {
		c.Name = "projects/" + c.ProjectID + "/locations/" + c.Location + "/channels/" + c.ChannelID
	}
	if c.UID == "" {
		c.UID = uuid.NewString()
	}
	if c.State == "" {
		c.State = "ACTIVE"
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	c.CreatedAt = now
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO eventarc_channels
		 (name, project_id, location, channel_id, uid, provider, pubsub_topic, state, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.Name, c.ProjectID, c.Location, c.ChannelID, c.UID, c.Provider, c.PubsubTopic, c.State, now,
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
	return &c, true, nil
}

// GetEventarcChannel loads by resource name.
func (s *Store) GetEventarcChannel(name string) (*EventarcChannel, bool, error) {
	var c EventarcChannel
	err := s.db.QueryRow(
		`SELECT name, project_id, location, channel_id, uid, provider, pubsub_topic, state, created_at
		 FROM eventarc_channels WHERE name = ?`, name,
	).Scan(&c.Name, &c.ProjectID, &c.Location, &c.ChannelID, &c.UID, &c.Provider, &c.PubsubTopic, &c.State, &c.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &c, true, nil
}

// ListEventarcChannels lists channels for project+location.
func (s *Store) ListEventarcChannels(projectID, location string) ([]EventarcChannel, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, location, channel_id, uid, provider, pubsub_topic, state, created_at
		 FROM eventarc_channels WHERE project_id = ? AND location = ? ORDER BY name`,
		projectID, location,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EventarcChannel
	for rows.Next() {
		var c EventarcChannel
		if err := rows.Scan(&c.Name, &c.ProjectID, &c.Location, &c.ChannelID, &c.UID, &c.Provider, &c.PubsubTopic, &c.State, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DeleteEventarcChannel removes a channel.
func (s *Store) DeleteEventarcChannel(name string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM eventarc_channels WHERE name = ?`, name)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

type eventFilter struct {
	Attribute string            `json:"attribute"`
	Value     string            `json:"value"`
	Operator  string            `json:"operator"`
	Values    map[string]string `json:"values"`
}

type eventDestination struct {
	HTTPEndpoint *struct {
		URI string `json:"uri"`
	} `json:"httpEndpoint"`
	CloudRunService *struct {
		Service        string `json:"service"`
		Region         string `json:"region"`
		Path           string `json:"path"`
		ServiceAccount string `json:"serviceAccount"`
	} `json:"cloudRunService"`
	// CloudFunction is a resource name string or an object with name/service+region.
	CloudFunction json.RawMessage `json:"cloudFunction"`
}

type eventTransport struct {
	Pubsub *struct {
		Topic string `json:"topic"`
	} `json:"pubsub"`
}

// DeliverEventarcForPubSub best-effort POSTs matching triggers for a Pub/Sub publish.
func (s *Store) DeliverEventarcForPubSub(topic string, data []byte, attributes map[string]string) {
	triggers, err := s.listAllEventarcTriggers()
	if err != nil {
		return
	}
	attrs := map[string]string{"type": "google.cloud.pubsub.topic.v1.messagePublished"}
	for k, v := range attributes {
		attrs[k] = v
	}
	payload := map[string]any{
		"specversion":     "1.0",
		"type":            "google.cloud.pubsub.topic.v1.messagePublished",
		"source":          "//pubsub.googleapis.com/" + topic,
		"id":              uuid.NewString(),
		"time":            time.Now().UTC().Format(time.RFC3339Nano),
		"datacontenttype": "application/json",
		"data": map[string]any{
			"message": map[string]any{
				"data":       data,
				"attributes": attributes,
			},
			"subscription": "",
		},
	}
	for _, t := range triggers {
		if !eventarcMatches(t, attrs, topic, "") {
			continue
		}
		s.deliverEventarc(t, payload)
	}
}

// DeliverEventarcForGCSFinalize best-effort POSTs matching triggers for object finalize.
func (s *Store) DeliverEventarcForGCSFinalize(bucket, objectName string, generation int64, size int64, contentType string) {
	triggers, err := s.listAllEventarcTriggers()
	if err != nil {
		return
	}
	attrs := map[string]string{
		"type":   "google.cloud.storage.object.v1.finalized",
		"bucket": bucket,
	}
	payload := map[string]any{
		"specversion":     "1.0",
		"type":            "google.cloud.storage.object.v1.finalized",
		"source":          "//storage.googleapis.com/projects/_/buckets/" + bucket,
		"id":              uuid.NewString(),
		"time":            time.Now().UTC().Format(time.RFC3339Nano),
		"datacontenttype": "application/json",
		"data": map[string]any{
			"bucket":      bucket,
			"name":        objectName,
			"generation":  strconv.FormatInt(generation, 10),
			"size":        strconv.FormatInt(size, 10),
			"contentType": contentType,
		},
	}
	for _, t := range triggers {
		if !eventarcMatches(t, attrs, "", bucket) {
			continue
		}
		s.deliverEventarc(t, payload)
	}
}

func (s *Store) listAllEventarcTriggers() ([]EventarcTrigger, error) {
	rows, err := s.db.Query(
		`SELECT name, project_id, location, trigger_id, filters_json, destination_json, transport_json, channel, COALESCE(service_account, ''), created_at
		 FROM eventarc_triggers`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EventarcTrigger
	for rows.Next() {
		var t EventarcTrigger
		if err := rows.Scan(&t.Name, &t.ProjectID, &t.Location, &t.TriggerID, &t.FiltersJSON, &t.DestinationJSON, &t.TransportJSON, &t.Channel, &t.ServiceAccount, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func eventarcMatches(t EventarcTrigger, attrs map[string]string, pubsubTopic, gcsBucket string) bool {
	var filters []eventFilter
	if err := json.Unmarshal([]byte(t.FiltersJSON), &filters); err != nil {
		return false
	}
	if len(filters) == 0 {
		return false
	}
	for _, f := range filters {
		switch f.Attribute {
		case "type":
			if attrs["type"] != f.Value {
				return false
			}
		case "bucket":
			if gcsBucket != f.Value && attrs["bucket"] != f.Value {
				return false
			}
		default:
			if len(f.Values) > 0 {
				for k, v := range f.Values {
					if attrs[k] != v {
						return false
					}
				}
				continue
			}
			if f.Value != "" && attrs[f.Attribute] != f.Value {
				return false
			}
		}
	}
	// Pub/Sub transport topic filter when present.
	var transport eventTransport
	_ = json.Unmarshal([]byte(t.TransportJSON), &transport)
	if transport.Pubsub != nil && transport.Pubsub.Topic != "" && pubsubTopic != "" {
		if transport.Pubsub.Topic != pubsubTopic {
			return false
		}
	}
	return true
}

func (s *Store) deliverEventarc(t EventarcTrigger, payload map[string]any) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	// Prefer in-process Cloud Functions delivery so lab Eventarc works without
	// Bearer mint (A2) and without a live :4588 listener in unit tests.
	if fnName := cloudFunctionNameFromDestination(t); fnName != "" {
		if _, ok, err := s.GetCloudFunction(fnName); err == nil && ok {
			RecordCloudFunctionInvoke(fnName, string(raw))
			return
		}
	}
	uri := s.resolveEventarcURI(t)
	if uri == "" {
		return
	}
	if err := httpegress.Validate(uri); err != nil {
		return
	}
	if u, err := url.Parse(uri); err == nil && httpegress.IsLabCatcher(u, strings.ToLower(u.Scheme)) {
		RecordHTTPCatcher(string(raw))
		return
	}
	// Lab-local Functions :invoke path: record in-process when the function exists.
	if fnName := functionNameFromInvokeURI(uri); fnName != "" {
		if _, ok, err := s.GetCloudFunction(fnName); err == nil && ok {
			RecordCloudFunctionInvoke(fnName, string(raw))
			return
		}
	}
	authHeader, authErr := s.eventarcAuthHeader(t, uri)
	if authErr != nil {
		log.Printf("eventarc deliver %s: %v", t.Name, authErr)
		return
	}
	client := httpegress.Client(3 * time.Second)
	doPost := func() error {
		req, err := http.NewRequest(http.MethodPost, uri, bytes.NewReader(raw))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("ce-specversion", "1.0")
		if typ, ok := payload["type"].(string); ok {
			req.Header.Set("ce-type", typ)
		}
		if authHeader != "" {
			req.Header.Set("Authorization", authHeader)
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		if resp.StatusCode >= 500 {
			return fmt.Errorf("status %d", resp.StatusCode)
		}
		return nil
	}
	if err := doPost(); err != nil {
		_ = doPost() // one retry on failed deliver
	}
}

// eventarcAuthHeader mints a registered lab Bearer for delivery to protected destinations.
// Prefer trigger.serviceAccount, then destination.cloudRunService.serviceAccount, else default compute SA.
// Fail closed when targeting lab :invoke without a resolvable SA.
func (s *Store) eventarcAuthHeader(t EventarcTrigger, uri string) (string, error) {
	email := strings.TrimSpace(t.ServiceAccount)
	if email == "" {
		var dest eventDestination
		_ = json.Unmarshal([]byte(t.DestinationJSON), &dest)
		if dest.CloudRunService != nil {
			email = strings.TrimSpace(dest.CloudRunService.ServiceAccount)
		}
	}
	if email == "" {
		email = labtoken.DefaultComputeSAEmail(t.ProjectID)
	}
	if email == "" {
		if isLabInvokeURI(uri) {
			return "", fmt.Errorf("no service account to mint Bearer for protected invoke destination")
		}
		return "", nil
	}
	if err := s.EnsureServiceAccount(t.ProjectID, email, "eventarc delivery SA"); err != nil {
		return "", fmt.Errorf("ensure service account: %w", err)
	}
	tok, _, err := labtoken.Mint(s, email, labtoken.DefaultLifetime)
	if err != nil {
		return "", err
	}
	return "Bearer " + tok, nil
}

func isLabInvokeURI(uri string) bool {
	u, err := url.Parse(uri)
	if err != nil {
		return strings.Contains(uri, ":invoke")
	}
	if !httpegress.IsLabLocal(u, strings.ToLower(u.Scheme)) {
		return false
	}
	return strings.Contains(uri, ":invoke")
}

func (s *Store) resolveEventarcURI(t EventarcTrigger) string {
	var dest eventDestination
	if err := json.Unmarshal([]byte(t.DestinationJSON), &dest); err != nil {
		return ""
	}
	if dest.HTTPEndpoint != nil && dest.HTTPEndpoint.URI != "" {
		return dest.HTTPEndpoint.URI
	}
	if dest.CloudRunService != nil && dest.CloudRunService.Service != "" {
		region := dest.CloudRunService.Region
		if region == "" {
			region = t.Location
		}
		path := dest.CloudRunService.Path
		if path == "" {
			path = "/"
		}
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		runName := fmt.Sprintf("projects/%s/locations/%s/services/%s", t.ProjectID, region, dest.CloudRunService.Service)
		if rs, ok, err := s.GetRunService(runName); err == nil && ok && rs.URI != "" {
			return strings.TrimRight(rs.URI, "/") + path
		}
		return fmt.Sprintf("http://127.0.0.1:4588/v2/projects/%s/locations/%s/services/%s:invoke%s",
			t.ProjectID, region, dest.CloudRunService.Service, path)
	}
	if fnName := parseCloudFunctionDestination(dest.CloudFunction, t); fnName != "" {
		if fn, ok, err := s.GetCloudFunction(fnName); err == nil && ok && fn.URI != "" {
			return fn.URI
		}
		return fmt.Sprintf("http://127.0.0.1:4588/v2/%s:invoke", fnName)
	}
	return ""
}

func cloudFunctionNameFromDestination(t EventarcTrigger) string {
	var dest eventDestination
	if err := json.Unmarshal([]byte(t.DestinationJSON), &dest); err != nil {
		return ""
	}
	return parseCloudFunctionDestination(dest.CloudFunction, t)
}

// parseCloudFunctionDestination accepts a resource name string or
// {"name"|"function"|"service","region"|"location"} object.
func parseCloudFunctionDestination(raw json.RawMessage, t EventarcTrigger) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return strings.TrimSpace(asString)
	}
	var asObj struct {
		Name     string `json:"name"`
		Function string `json:"function"`
		Service  string `json:"service"`
		Region   string `json:"region"`
		Location string `json:"location"`
	}
	if err := json.Unmarshal(raw, &asObj); err != nil {
		return ""
	}
	if n := strings.TrimSpace(asObj.Name); n != "" {
		return n
	}
	id := strings.TrimSpace(asObj.Function)
	if id == "" {
		id = strings.TrimSpace(asObj.Service)
	}
	if id == "" {
		return ""
	}
	region := strings.TrimSpace(asObj.Region)
	if region == "" {
		region = strings.TrimSpace(asObj.Location)
	}
	if region == "" {
		region = t.Location
	}
	return fmt.Sprintf("projects/%s/locations/%s/functions/%s", t.ProjectID, region, id)
}

// functionNameFromInvokeURI extracts projects/.../functions/{id} from a lab :invoke URI.
func functionNameFromInvokeURI(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return ""
	}
	path := strings.TrimSuffix(u.Path, "/")
	const marker = "/functions/"
	i := strings.Index(path, marker)
	if i < 0 {
		return ""
	}
	rest := path[i+len(marker):]
	id, action, ok := strings.Cut(rest, ":")
	if !ok || action != "invoke" || id == "" {
		return ""
	}
	prefix := path[:i]
	if !strings.Contains(prefix, "/projects/") {
		return ""
	}
	// path like /v2/projects/{p}/locations/{loc}/functions/{id}:invoke
	if idx := strings.Index(prefix, "/projects/"); idx >= 0 {
		prefix = prefix[idx+1:] // drop leading slash → projects/...
	}
	return prefix + "/functions/" + id
}
