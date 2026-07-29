package logging

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// EnvAuditInject enables POST /_noctaxris-gcp/lab/auditLogs:inject (default off).
const EnvAuditInject = "NOCTAXRIS_GCP_AUDIT_INJECT"

const labAuditInjectPath = "/_noctaxris-gcp/lab/auditLogs:inject"

// MountLab registers the env-gated Cloud Audit Logs inject route.
// Inject requires EnvAuditInject=1 (or true) and a Bearer root principal.
func (s *Service) MountLab(mux *http.ServeMux, principalFrom principalFunc, defaultProject string) {
	mux.HandleFunc("POST "+labAuditInjectPath, s.wrap(principalFrom, func(w http.ResponseWriter, r *http.Request, p authn.Principal) {
		s.injectAuditLogs(w, r, p, defaultProject)
	}))
}

// AuditInjectEnabled reports whether lab CAL inject is enabled.
func AuditInjectEnabled() bool {
	v := strings.TrimSpace(os.Getenv(EnvAuditInject))
	return strings.EqualFold(v, "1") || strings.EqualFold(v, "true")
}

type injectReq struct {
	ProjectID string            `json:"projectId"`
	LogName   string            `json:"logName"`
	Entries   []injectEntryIn   `json:"entries"`
	Entry     *injectEntryIn    `json:"entry"`
}

type injectEntryIn struct {
	LogName      string          `json:"logName"`
	InsertID     string          `json:"insertId"`
	Severity     string          `json:"severity"`
	Timestamp    string          `json:"timestamp"`
	ProtoPayload json.RawMessage `json:"protoPayload"`
	Resource     json.RawMessage `json:"resource"`
	// Flat lite fields when protoPayload is omitted.
	ServiceName    string `json:"serviceName"`
	MethodName     string `json:"methodName"`
	ResourceName   string `json:"resourceName"`
	PrincipalEmail string `json:"principalEmail"`
	Permission     string `json:"permission"`
	Granted        *bool  `json:"granted"`
	StatusCode     int    `json:"statusCode"`
}

func (s *Service) injectAuditLogs(w http.ResponseWriter, r *http.Request, p authn.Principal, defaultProject string) {
	if !AuditInjectEnabled() {
		gcperrors.PermissionDenied(w, "Cloud Audit Logs lab inject is disabled. Set NOCTAXRIS_GCP_AUDIT_INJECT=1 to enable.")
		return
	}
	if !p.IsRoot {
		gcperrors.PermissionDenied(w, "Cloud Audit Logs lab inject requires Bearer root")
		return
	}

	var req injectReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	entries := req.Entries
	if req.Entry != nil {
		entries = append(entries, *req.Entry)
	}
	if len(entries) == 0 {
		gcperrors.InvalidArgument(w, "entries or entry is required")
		return
	}
	if len(entries) > 50 {
		gcperrors.InvalidArgument(w, "at most 50 entries per inject")
		return
	}

	projectID := strings.TrimSpace(req.ProjectID)
	if projectID == "" {
		projectID = strings.TrimSpace(defaultProject)
	}
	now := time.Now().UTC()
	out := make([]store.CloudAuditEntry, 0, len(entries))
	for i, e := range entries {
		logName := strings.TrimSpace(e.LogName)
		if logName == "" {
			logName = strings.TrimSpace(req.LogName)
		}
		if logName == "" {
			if projectID == "" {
				gcperrors.InvalidArgument(w, "projectId or logName is required")
				return
			}
			logName = store.CloudAuditLogName(projectID, store.CloudAuditLogIDActivity)
		}
		pid, err := projectFromLogName(logName)
		if err != nil {
			gcperrors.InvalidArgument(w, err.Error())
			return
		}
		if projectID == "" {
			projectID = pid
		}
		if pid != projectID {
			gcperrors.InvalidArgument(w, "logName project must match projectId")
			return
		}
		if !store.IsCloudAuditLogName(logName) {
			gcperrors.InvalidArgument(w, "logName must be a cloudaudit.googleapis.com log")
			return
		}

		protoJSON, err := buildInjectProtoPayload(e)
		if err != nil {
			gcperrors.InvalidArgument(w, err.Error())
			return
		}
		insertID := e.InsertID
		if insertID == "" {
			insertID = newInsertID(i)
		}
		ts := e.Timestamp
		if ts == "" {
			ts = now.Add(time.Duration(i) * time.Millisecond).Format(time.RFC3339Nano)
		}
		sev := e.Severity
		if sev == "" {
			sev = "NOTICE"
		}
		resJSON := e.Resource
		if len(resJSON) == 0 {
			resJSON = json.RawMessage(`{"type":"audited_resource"}`)
		}
		out = append(out, store.CloudAuditEntry{
			InsertID:         insertID,
			ProjectID:        projectID,
			LogName:          logName,
			Severity:         sev,
			Timestamp:        ts,
			ProtoPayloadJSON: string(protoJSON),
			ResourceJSON:     string(resJSON),
		})
	}

	if err := s.Store.WriteCloudAuditEntries(out); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"written": len(out),
		"entries": len(out),
	})
}

func buildInjectProtoPayload(e injectEntryIn) ([]byte, error) {
	if len(e.ProtoPayload) > 0 && string(e.ProtoPayload) != "null" {
		var proto map[string]any
		if err := json.Unmarshal(e.ProtoPayload, &proto); err != nil {
			return nil, fmt.Errorf("invalid protoPayload JSON")
		}
		if _, ok := proto["@type"]; !ok {
			proto["@type"] = store.CloudAuditProtoPayloadType
		}
		return json.Marshal(proto)
	}
	if e.ServiceName == "" && e.MethodName == "" {
		return nil, fmt.Errorf("protoPayload or serviceName/methodName is required")
	}
	proto := map[string]any{
		"@type":        store.CloudAuditProtoPayloadType,
		"serviceName":  e.ServiceName,
		"methodName":   e.MethodName,
		"resourceName": e.ResourceName,
		"authenticationInfo": map[string]any{
			"principalEmail": e.PrincipalEmail,
		},
	}
	if e.Permission != "" || e.Granted != nil {
		authzInfo := map[string]any{"permission": e.Permission}
		if e.Granted != nil {
			authzInfo["granted"] = *e.Granted
		}
		proto["authorizationInfo"] = []any{authzInfo}
	}
	if e.StatusCode != 0 {
		proto["status"] = map[string]any{"code": e.StatusCode}
	}
	return json.Marshal(proto)
}

func wantsCloudAudit(exactLogName, filter string) bool {
	if store.IsCloudAuditLogName(exactLogName) {
		return true
	}
	return strings.Contains(strings.ToLower(filter), "cloudaudit.googleapis.com")
}
