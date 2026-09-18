package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

const (
	labClockFreezePath   = "/_noctaxris-gcp/lab/clock:freeze"
	labClockUnfreezePath = "/_noctaxris-gcp/lab/clock:unfreeze"
	labClockSetPath      = "/_noctaxris-gcp/lab/clock:set"
	labBulkSeedPath      = "/_noctaxris-gcp/lab/bulkSeed"
)

func (s *Server) effectiveNow() time.Time {
	s.clockMu.RLock()
	defer s.clockMu.RUnlock()
	return s.labClockNowLocked()
}

// authClock is wall time for Bearer expiry and signature skew (lab clock must not affect auth).
func (s *Server) authClock() time.Time {
	return time.Now().UTC()
}

func (s *Server) labClockNowLocked() time.Time {
	if s.clockOverride != nil {
		return s.clockOverride.UTC()
	}
	return time.Now().UTC()
}

func (s *Server) freezeLabClock() time.Time {
	s.clockMu.Lock()
	defer s.clockMu.Unlock()
	t := s.labClockNowLocked()
	s.clockOverride = &t
	return t
}

func (s *Server) setLabClock(t time.Time) time.Time {
	utc := t.UTC()
	s.clockMu.Lock()
	defer s.clockMu.Unlock()
	s.clockOverride = &utc
	return utc
}

func (s *Server) unfreezeLabClock() {
	s.clockMu.Lock()
	defer s.clockMu.Unlock()
	s.clockOverride = nil
}

func (s *Server) registerLabForensics() {
	s.mux.HandleFunc("POST "+labClockFreezePath, s.handleLabClockFreeze)
	s.mux.HandleFunc("POST "+labClockUnfreezePath, s.handleLabClockUnfreeze)
	s.mux.HandleFunc("POST "+labClockSetPath, s.handleLabClockSet)
	s.mux.HandleFunc("POST "+labBulkSeedPath, s.handleLabBulkSeed)
	s.mux.HandleFunc("GET /computeMetadata/v1/", s.handleComputeMetadata)
	s.mux.HandleFunc("GET /computeMetadata/v1", s.handleComputeMetadata)
}

func (s *Server) requireLabForensicsRoot(w http.ResponseWriter, r *http.Request) bool {
	if !s.cfg.LabForensics {
		gcperrors.PermissionDenied(w, "Lab forensics is disabled. Set NOCTAXRIS_GCP_LAB_FORENSICS=1 to enable.")
		return false
	}
	p, ok := PrincipalFromContext(r.Context())
	if !ok {
		gcperrors.Unauthenticated(w, "")
		return false
	}
	if !p.IsRoot {
		gcperrors.PermissionDenied(w, "Lab forensics requires Bearer root")
		return false
	}
	return true
}

func (s *Server) handleLabClockFreeze(w http.ResponseWriter, r *http.Request) {
	if !s.requireLabForensicsRoot(w, r) {
		return
	}
	t := s.freezeLabClock()
	writeJSON(w, http.StatusOK, map[string]any{
		"clockTime": t.Format(time.RFC3339),
		"frozen":    true,
	})
}

func (s *Server) handleLabClockUnfreeze(w http.ResponseWriter, r *http.Request) {
	if !s.requireLabForensicsRoot(w, r) {
		return
	}
	s.unfreezeLabClock()
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Server) handleLabClockSet(w http.ResponseWriter, r *http.Request) {
	if !s.requireLabForensicsRoot(w, r) {
		return
	}
	var body struct {
		FixedTime string `json:"fixedTime"`
		ClockTime string `json:"clockTime"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	raw := strings.TrimSpace(body.FixedTime)
	if raw == "" {
		raw = strings.TrimSpace(body.ClockTime)
	}
	if raw == "" {
		gcperrors.InvalidArgument(w, "fixedTime is required (RFC3339)")
		return
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t, err = time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			gcperrors.InvalidArgument(w, "fixedTime must be RFC3339")
			return
		}
	}
	set := s.setLabClock(t)
	writeJSON(w, http.StatusOK, map[string]any{
		"clockTime": set.Format(time.RFC3339),
	})
}

func (s *Server) handleLabBulkSeed(w http.ResponseWriter, r *http.Request) {
	if !s.requireLabForensicsRoot(w, r) {
		return
	}
	var body struct {
		ScenarioID string `json:"scenarioId"`
		ProjectID  string `json:"projectId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	scenarioID := strings.ToLower(strings.TrimSpace(body.ScenarioID))
	if scenarioID == "" {
		gcperrors.InvalidArgument(w, "scenarioId is required")
		return
	}
	projectID := strings.TrimSpace(body.ProjectID)
	if projectID == "" {
		projectID = s.cfg.ProjectID
	}
	base := s.effectiveNow().UTC()
	entries, logs, err := labScenarioPack(projectID, scenarioID, base)
	if err != nil {
		gcperrors.InvalidArgument(w, err.Error())
		return
	}
	if err := s.store.WriteCloudAuditEntries(entries); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if len(logs) > 0 {
		if err := s.store.WriteLogEntries(logs); err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		}
	}
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.InsertID)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"scenarioId":             scenarioID,
		"cloudAuditEventCount":   len(entries),
		"cloudAuditInsertIds":    ids,
		"logEntryCount":          len(logs),
	})
}

func labScenarioPack(projectID, scenarioID string, base time.Time) ([]store.CloudAuditEntry, []store.LogEntry, error) {
	switch scenarioID {
	case "suspicious-login":
		return labScenarioSuspiciousLogin(projectID, base), nil, nil
	case "s3-data-exfil":
		return labScenarioGCSExfil(projectID, base)
	case "crypto-mining":
		return labScenarioCryptoMining(projectID, base)
	default:
		return nil, nil, fmt.Errorf("unknown scenarioId %q (known: suspicious-login, s3-data-exfil, crypto-mining)", scenarioID)
	}
}

func labScenarioSuspiciousLogin(projectID string, base time.Time) []store.CloudAuditEntry {
	t := base.Add(-2 * time.Hour).Format(time.RFC3339Nano)
	proto, _ := json.Marshal(map[string]any{
		"@type":        store.CloudAuditProtoPayloadType,
		"serviceName":  "login.googleapis.com",
		"methodName":   "google.login.LoginService.loginSuccess",
		"resourceName": "projects/" + projectID,
		"authenticationInfo": map[string]any{
			"principalEmail": "contractor@" + projectID + ".iam.gserviceaccount.com",
		},
		"requestMetadata": map[string]any{"callerIp": "203.0.113.44"},
	})
	return []store.CloudAuditEntry{{
		InsertID:         "seed-login-1",
		ProjectID:        projectID,
		LogName:          store.CloudAuditLogName(projectID, store.CloudAuditLogIDActivity),
		Severity:         "NOTICE",
		Timestamp:        t,
		ProtoPayloadJSON: string(proto),
		ResourceJSON:     `{"type":"audited_resource"}`,
	}}
}

func labScenarioGCSExfil(projectID string, base time.Time) ([]store.CloudAuditEntry, []store.LogEntry, error) {
	t1 := base.Add(-45 * time.Minute).Format(time.RFC3339Nano)
	t2 := base.Add(-30 * time.Minute).Format(time.RFC3339Nano)
	bucket := "corp-sensitive-" + projectID
	listProto, _ := json.Marshal(map[string]any{
		"@type":        store.CloudAuditProtoPayloadType,
		"serviceName":  "storage.googleapis.com",
		"methodName":   "storage.buckets.list",
		"resourceName": "projects/_/buckets",
		"authenticationInfo": map[string]any{
			"principalEmail": "investigator@" + projectID + ".iam.gserviceaccount.com",
		},
	})
	getProto, _ := json.Marshal(map[string]any{
		"@type":        store.CloudAuditProtoPayloadType,
		"serviceName":  "storage.googleapis.com",
		"methodName":   "storage.objects.get",
		"resourceName": "projects/_/buckets/" + bucket + "/objects/finance/q1-report.csv",
		"authenticationInfo": map[string]any{
			"principalEmail": "investigator@" + projectID + ".iam.gserviceaccount.com",
		},
	})
	cal := []store.CloudAuditEntry{
		{
			InsertID: "seed-gcs-list-1", ProjectID: projectID,
			LogName: store.CloudAuditLogName(projectID, store.CloudAuditLogIDActivity),
			Severity: "NOTICE", Timestamp: t1, ProtoPayloadJSON: string(listProto),
			ResourceJSON: `{"type":"gcs_bucket"}`,
		},
		{
			InsertID: "seed-gcs-get-1", ProjectID: projectID,
			LogName: store.CloudAuditLogName(projectID, store.CloudAuditLogIDDataAccess),
			Severity: "INFO", Timestamp: t2, ProtoPayloadJSON: string(getProto),
			ResourceJSON: `{"type":"gcs_bucket","labels":{"bucket_name":"` + bucket + `"}}`,
		},
	}
	payload, _ := json.Marshal(map[string]any{
		"jsonPayload": map[string]any{"object": "finance/q1-report.csv", "bytes": 4096},
	})
	logs := []store.LogEntry{{
		InsertID: "seed-gcs-log-1", ProjectID: projectID,
		LogName: "projects/" + projectID + "/logs/storage.googleapis.com%2Fdata_access",
		Severity: "INFO", Timestamp: t2, PayloadJSON: string(payload),
		ResourceJSON: `{"type":"gcs_bucket","labels":{"bucket_name":"` + bucket + `"}}`,
	}}
	return cal, logs, nil
}

func labScenarioCryptoMining(projectID string, base time.Time) ([]store.CloudAuditEntry, []store.LogEntry, error) {
	t := base.Add(-15 * time.Minute).Format(time.RFC3339Nano)
	proto, _ := json.Marshal(map[string]any{
		"@type":        store.CloudAuditProtoPayloadType,
		"serviceName":  "compute.googleapis.com",
		"methodName":   "v1.compute.instances.insert",
		"resourceName": "projects/" + projectID + "/zones/us-central1-a/instances/miner",
		"authenticationInfo": map[string]any{
			"principalEmail": "devops@" + projectID + ".iam.gserviceaccount.com",
		},
		"request": map[string]any{"machineType": "c2-standard-16"},
	})
	cal := []store.CloudAuditEntry{{
		InsertID: "seed-gce-run-1", ProjectID: projectID,
		LogName: store.CloudAuditLogName(projectID, store.CloudAuditLogIDActivity),
		Severity: "NOTICE", Timestamp: t, ProtoPayloadJSON: string(proto),
		ResourceJSON: `{"type":"gce_instance"}`,
	}}
	return cal, nil, nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func isGOOG4HMACAuth(auth string) bool {
	return strings.HasPrefix(strings.TrimSpace(auth), store.LabGCSSignAlgo)
}

func rewriteLabHostPath(r *http.Request) {
	host := strings.ToLower(strings.TrimSpace(r.Host))
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	path := r.URL.Path
	switch host {
	case "iamcredentials.googleapis.com":
		if !strings.HasPrefix(path, "/iamcredentials.googleapis.com/") {
			r.URL.Path = "/iamcredentials.googleapis.com" + path
		}
	case "storage.googleapis.com":
		if strings.HasPrefix(path, "/storage/") || strings.HasPrefix(path, "/upload/storage/") {
			return
		}
		if !strings.HasPrefix(path, "/storage/xml/") {
			r.URL.Path = "/storage/xml" + path
		}
	}
}

func (s *Server) handleComputeMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Metadata-Flavor") != "Google" {
		gcperrors.Unauthenticated(w, "Metadata-Flavor: Google is required")
		return
	}
	project := s.cfg.ProjectID
	if project == "" {
		project = "noctaxris-gcp-local"
	}
	email := "runtime@" + project + ".iam.gserviceaccount.com"
	path := strings.TrimPrefix(r.URL.Path, "/computeMetadata/v1")
	path = strings.Trim(path, "/")
	w.Header().Set("Metadata-Flavor", "Google")
	switch {
	case path == "" || path == "instance" || path == "instance/service-accounts" || path == "instance/service-accounts/":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("default/\n" + email + "/\n"))
	case path == "instance/service-accounts/default" || path == "instance/service-accounts/default/" ||
		path == "instance/service-accounts/"+email || path == "instance/service-accounts/"+email+"/":
		writeJSON(w, http.StatusOK, map[string]any{
			"aliases": []string{"default"},
			"email":   email,
			"scopes":  []string{"https://www.googleapis.com/auth/cloud-platform"},
		})
	case strings.HasSuffix(path, "/email"):
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(email))
	case strings.HasSuffix(path, "/token"):
		token := "lab-metadata-" + strings.ReplaceAll(email, "@", "-")
		expire := s.authClock().Add(time.Hour)
		if err := s.store.PutAccessToken(authn.HashToken(token), email, expire); err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"access_token": token,
			"expires_in":   3600,
			"token_type":   "Bearer",
		})
	default:
		gcperrors.NotFound(w, "metadata path not found")
	}
}
