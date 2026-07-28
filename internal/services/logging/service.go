package logging

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// Service serves Cloud Logging v2 REST (lab subset).
type Service struct {
	Store *store.Store
	Authz *authz.Evaluator
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// Mount registers Logging REST routes.
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	mux.HandleFunc("POST /v2/entries:write", s.wrap(principalFrom, s.writeEntries))
	mux.HandleFunc("POST /v2/entries:list", s.wrap(principalFrom, s.listEntries))
}

type handlerFunc func(w http.ResponseWriter, r *http.Request, p authn.Principal)

func (s *Service) wrap(principalFrom principalFunc, h handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := principalFrom(r)
		if !ok {
			gcperrors.Unauthenticated(w, "")
			return
		}
		h(w, r, p)
	}
}

func (s *Service) require(p authn.Principal, permission, projectID string) error {
	ok, err := s.Authz.Evaluate(p.Email, p.IsRoot, permission, "projects/"+projectID)
	if err != nil {
		return err
	}
	if !ok {
		return errDenied
	}
	return nil
}

var errDenied = fmt.Errorf("permission denied")

type writeReq struct {
	LogName  string          `json:"logName"`
	Resource json.RawMessage `json:"resource"`
	Entries  []logEntryIn    `json:"entries"`
}

type logEntryIn struct {
	LogName      string          `json:"logName"`
	InsertID     string          `json:"insertId"`
	Severity     string          `json:"severity"`
	Timestamp    string          `json:"timestamp"`
	TextPayload  string          `json:"textPayload"`
	JSONPayload  json.RawMessage `json:"jsonPayload"`
	Resource     json.RawMessage `json:"resource"`
}

type listReq struct {
	ResourceNames []string `json:"resourceNames"`
	ProjectIds    []string `json:"projectIds"`
	Filter        string   `json:"filter"`
	PageSize      int      `json:"pageSize"`
	PageToken     string   `json:"pageToken"`
	OrderBy       string   `json:"orderBy"`
}

var (
	reLogNameExact = regexp.MustCompile(`(?i)^logName\s*=\s*"([^"]+)"$`)
	reTextContains = regexp.MustCompile(`(?i)^textPayload\s*:\s*"([^"]+)"$`)
	reLogNameEq    = regexp.MustCompile(`(?i)logName\s*=\s*"([^"]+)"`)
	reTextColon    = regexp.MustCompile(`(?i)textPayload\s*:\s*"([^"]+)"`)
)

func (s *Service) writeEntries(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	var req writeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	if len(req.Entries) == 0 {
		gcperrors.InvalidArgument(w, "entries is required")
		return
	}
	now := time.Now().UTC()
	out := make([]store.LogEntry, 0, len(req.Entries))
	for i, e := range req.Entries {
		logName := e.LogName
		if logName == "" {
			logName = req.LogName
		}
		if logName == "" {
			gcperrors.InvalidArgument(w, "logName is required")
			return
		}
		projectID, err := projectFromLogName(logName)
		if err != nil {
			gcperrors.InvalidArgument(w, err.Error())
			return
		}
		if err := s.require(p, "logging.logEntries.create", projectID); err != nil {
			if err == errDenied {
				gcperrors.PermissionDenied(w, "")
				return
			}
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
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
		payload := map[string]any{}
		if e.TextPayload != "" {
			payload["textPayload"] = e.TextPayload
		}
		if len(e.JSONPayload) > 0 && string(e.JSONPayload) != "null" {
			var jp any
			if err := json.Unmarshal(e.JSONPayload, &jp); err == nil {
				payload["jsonPayload"] = jp
			}
		}
		if len(payload) == 0 {
			payload["textPayload"] = ""
		}
		payloadJSON, _ := json.Marshal(payload)
		resJSON := e.Resource
		if len(resJSON) == 0 {
			resJSON = req.Resource
		}
		if len(resJSON) == 0 {
			resJSON = json.RawMessage(`{}`)
		}
		sev := e.Severity
		if sev == "" {
			sev = "DEFAULT"
		}
		out = append(out, store.LogEntry{
			InsertID: insertID, ProjectID: projectID, LogName: logName,
			Severity: sev, Timestamp: ts, PayloadJSON: string(payloadJSON), ResourceJSON: string(resJSON),
		})
	}
	if err := s.Store.WriteLogEntries(out); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("{}"))
}

func (s *Service) listEntries(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	var req listReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	projectID := ""
	for _, rn := range req.ResourceNames {
		if strings.HasPrefix(rn, "projects/") {
			parts := strings.Split(rn, "/")
			if len(parts) >= 2 {
				projectID = parts[1]
				break
			}
		}
	}
	if projectID == "" && len(req.ProjectIds) > 0 {
		projectID = req.ProjectIds[0]
	}
	if projectID == "" {
		gcperrors.InvalidArgument(w, "resourceNames or projectIds is required")
		return
	}
	if err := s.require(p, "logging.logEntries.list", projectID); err != nil {
		if err == errDenied {
			gcperrors.PermissionDenied(w, "")
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 1000 {
		pageSize = 1000
	}
	offset := 0
	if req.PageToken != "" {
		n, err := strconv.Atoi(req.PageToken)
		if err != nil || n < 0 {
			gcperrors.InvalidArgument(w, "invalid pageToken")
			return
		}
		offset = n
	}
	exactLog, textContains := parseFilter(req.Filter)
	entries, err := s.Store.ListLogEntries(store.ListLogEntriesFilter{
		ProjectID: projectID, ExactLogName: exactLog, TextPayloadContain: textContains,
		PageSize: pageSize, Offset: offset,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		item := map[string]any{
			"insertId":  e.InsertID,
			"logName":   e.LogName,
			"severity":  e.Severity,
			"timestamp": e.Timestamp,
		}
		var payload map[string]any
		_ = json.Unmarshal([]byte(e.PayloadJSON), &payload)
		if tp, ok := payload["textPayload"]; ok {
			item["textPayload"] = tp
		}
		if jp, ok := payload["jsonPayload"]; ok {
			item["jsonPayload"] = jp
		}
		if e.ResourceJSON != "" && e.ResourceJSON != "{}" {
			var res any
			if json.Unmarshal([]byte(e.ResourceJSON), &res) == nil {
				item["resource"] = res
			}
		}
		out = append(out, item)
	}
	resp := map[string]any{"entries": out}
	if len(entries) == pageSize {
		resp["nextPageToken"] = strconv.Itoa(offset + pageSize)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func parseFilter(filter string) (exactLogName, textContains string) {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return "", ""
	}
	if m := reLogNameExact.FindStringSubmatch(filter); len(m) == 2 {
		return m[1], ""
	}
	if m := reTextContains.FindStringSubmatch(filter); len(m) == 2 {
		return "", m[1]
	}
	if m := reLogNameEq.FindStringSubmatch(filter); len(m) == 2 {
		exactLogName = m[1]
	}
	if m := reTextColon.FindStringSubmatch(filter); len(m) == 2 {
		textContains = m[1]
	}
	return exactLogName, textContains
}

func projectFromLogName(logName string) (string, error) {
	// projects/{project}/logs/{log}
	parts := strings.Split(logName, "/")
	if len(parts) < 2 || parts[0] != "projects" || parts[1] == "" {
		return "", fmt.Errorf("invalid logName %q", logName)
	}
	return parts[1], nil
}

func newInsertID(i int) string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%s-%d", hex.EncodeToString(b[:]), i)
}
