package logging

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
	"github.com/google/uuid"
)

// Service serves Cloud Logging v2 REST (lab subset).
type Service struct {
	Store *store.Store
	Authz *authz.Evaluator
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// Mount registers Logging REST routes.
// Colon custom methods (entries:write / entries:list) are literal path segments because
// ServeMux wildcards cannot embed ':' inside a pattern segment.
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	mux.HandleFunc("POST /v2/entries:write", s.wrap(principalFrom, s.writeEntries))
	mux.HandleFunc("POST /v2/entries:list", s.wrap(principalFrom, s.listEntries))
	mux.HandleFunc("POST /v2/entries:tail", s.wrap(principalFrom, s.tailEntries))
	mux.HandleFunc("POST /v2/entries:copy", s.wrap(principalFrom, s.copyEntries))
	mux.HandleFunc("GET /v2/projects/{project}/logs", s.wrap(principalFrom, s.listLogs))
	mux.HandleFunc("DELETE /v2/projects/{project}/logs/{log}", s.wrap(principalFrom, s.deleteLog))
	mux.HandleFunc("POST /v2/projects/{project}/sinks", s.wrap(principalFrom, s.createSink))
	mux.HandleFunc("GET /v2/projects/{project}/sinks", s.wrap(principalFrom, s.listSinks))
	mux.HandleFunc("GET /v2/projects/{project}/sinks/{sink}", s.wrap(principalFrom, s.getSink))
	mux.HandleFunc("PUT /v2/projects/{project}/sinks/{sink}", s.wrap(principalFrom, s.updateSink))
	mux.HandleFunc("PATCH /v2/projects/{project}/sinks/{sink}", s.wrap(principalFrom, s.updateSink))
	mux.HandleFunc("DELETE /v2/projects/{project}/sinks/{sink}", s.wrap(principalFrom, s.deleteSink))
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

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

type writeReq struct {
	LogName  string          `json:"logName"`
	Resource json.RawMessage `json:"resource"`
	Entries  []logEntryIn    `json:"entries"`
}

type logEntryIn struct {
	LogName     string          `json:"logName"`
	InsertID    string          `json:"insertId"`
	Severity    string          `json:"severity"`
	Timestamp   string          `json:"timestamp"`
	TextPayload string          `json:"textPayload"`
	JSONPayload json.RawMessage `json:"jsonPayload"`
	Resource    json.RawMessage `json:"resource"`
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
	reLogNameExact  = regexp.MustCompile(`(?i)^logName\s*=\s*"([^"]+)"$`)
	reTextContains  = regexp.MustCompile(`(?i)^textPayload\s*:\s*"([^"]+)"$`)
	reLogNameEq     = regexp.MustCompile(`(?i)logName\s*=\s*"([^"]+)"`)
	reTextColon     = regexp.MustCompile(`(?i)textPayload\s*:\s*"([^"]+)"`)
	reSeverityExact = regexp.MustCompile(`(?i)severity\s*=\s*"?([A-Za-z]+)"?`)
	reTimestampGTE  = regexp.MustCompile(`(?i)timestamp\s*>=\s*"([^"]+)"`)
	reTimestampGT   = regexp.MustCompile(`(?i)timestamp\s*>\s*"([^"]+)"`)
	reTimestampLT   = regexp.MustCompile(`(?i)timestamp\s*<\s*"([^"]+)"`)
	reTimestampLTE  = regexp.MustCompile(`(?i)timestamp\s*<=\s*"([^"]+)"`)
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
	projectID, err := projectFromListReq(req)
	if err != nil {
		gcperrors.InvalidArgument(w, err.Error())
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
	entries, err := s.queryEntries(projectID, req.Filter, pageSize, offset)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	resp := map[string]any{"entries": entriesToMaps(entries)}
	if len(entries) == pageSize {
		resp["nextPageToken"] = strconv.Itoa(offset + pageSize)
	}
	writeJSON(w, http.StatusOK, resp)
}

// tailEntries is a one-shot lab stand-in for TailLogEntries (no streaming).
func (s *Service) tailEntries(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	var req listReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	projectID, err := projectFromListReq(req)
	if err != nil {
		gcperrors.InvalidArgument(w, err.Error())
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
	entries, err := s.queryEntries(projectID, req.Filter, pageSize, 0)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	// One-shot theatre: return a single non-streaming response with current matches.
	writeJSON(w, http.StatusOK, map[string]any{
		"entries": entriesToMaps(entries),
		"labNote": "TailLogEntries is one-shot (no stream) in this lab",
	})
}

// copyEntries stores no export; returns a completed LRO theatre response.
func (s *Service) copyEntries(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	var req struct {
		Name          string   `json:"name"`
		Destination   string   `json:"destination"`
		Filter        string   `json:"filter"`
		ResourceNames []string `json:"resourceNames"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
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
	if projectID == "" && strings.HasPrefix(req.Name, "projects/") {
		parts := strings.Split(req.Name, "/")
		if len(parts) >= 2 {
			projectID = parts[1]
		}
	}
	if projectID == "" {
		gcperrors.InvalidArgument(w, "resourceNames or name with project is required")
		return
	}
	if err := s.require(p, "logging.entries.copy", projectID); err != nil {
		if err == errDenied {
			gcperrors.PermissionDenied(w, "")
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	opName := fmt.Sprintf("projects/%s/locations/global/operations/copy-%s", projectID, uuid.NewString())
	writeJSON(w, http.StatusOK, map[string]any{
		"name": opName,
		"done": true,
		"response": map[string]any{
			"@type":       "type.googleapis.com/google.logging.v2.CopyLogEntriesResponse",
			"logEntries":  0,
			"destination": req.Destination,
			"filter":      req.Filter,
			"labNote":     "entries.copy is metadata theatre; no export performed",
		},
	})
}

func (s *Service) createSink(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "logging.sinks.create", project); err != nil {
		writeAuthz(w, err)
		return
	}
	sinkID := r.URL.Query().Get("sinkId")
	if sinkID == "" {
		gcperrors.InvalidArgument(w, "sinkId is required")
		return
	}
	var body struct {
		Destination string `json:"destination"`
		Filter      string `json:"filter"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	sk, created, err := s.Store.CreateLogSink(store.LogSink{
		ProjectID: project, SinkID: sinkID, Destination: body.Destination, Filter: body.Filter,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "sink already exists")
		return
	}
	writeJSON(w, http.StatusOK, sinkResource(sk))
}

func (s *Service) getSink(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	sinkID := r.PathValue("sink")
	if err := s.require(p, "logging.sinks.get", project); err != nil {
		writeAuthz(w, err)
		return
	}
	name := "projects/" + project + "/sinks/" + sinkID
	sk, ok, err := s.Store.GetLogSink(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "sink not found")
		return
	}
	writeJSON(w, http.StatusOK, sinkResource(sk))
}

func (s *Service) listSinks(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "logging.sinks.list", project); err != nil {
		writeAuthz(w, err)
		return
	}
	list, err := s.Store.ListLogSinks(project)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(list))
	for i := range list {
		out = append(out, sinkResource(&list[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"sinks": out})
}

func (s *Service) updateSink(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	sinkID := r.PathValue("sink")
	if err := s.require(p, "logging.sinks.update", project); err != nil {
		writeAuthz(w, err)
		return
	}
	name := "projects/" + project + "/sinks/" + sinkID
	existing, ok, err := s.Store.GetLogSink(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "sink not found")
		return
	}
	var body struct {
		Destination *string `json:"destination"`
		Filter      *string `json:"filter"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	dest, filter := existing.Destination, existing.Filter
	if body.Destination != nil {
		dest = *body.Destination
	}
	if body.Filter != nil {
		filter = *body.Filter
	}
	sk, ok, err := s.Store.UpdateLogSink(name, dest, filter)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "sink not found")
		return
	}
	writeJSON(w, http.StatusOK, sinkResource(sk))
}

func (s *Service) deleteSink(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	sinkID := r.PathValue("sink")
	if err := s.require(p, "logging.sinks.delete", project); err != nil {
		writeAuthz(w, err)
		return
	}
	name := "projects/" + project + "/sinks/" + sinkID
	ok, err := s.Store.DeleteLogSink(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "sink not found")
		return
	}
	w.WriteHeader(http.StatusOK)
}

func sinkResource(sk *store.LogSink) map[string]any {
	return map[string]any{
		"name":           sk.Name,
		"destination":    sk.Destination,
		"filter":         sk.Filter,
		"writerIdentity": sk.WriterIdentity,
		"createTime":     sk.CreatedAt,
		"updateTime":     sk.UpdatedAt,
	}
}

func (s *Service) deleteLog(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	logID := r.PathValue("log")
	if project == "" || logID == "" {
		gcperrors.InvalidArgument(w, "project and log are required")
		return
	}
	if err := s.require(p, "logging.logs.delete", project); err != nil {
		if err == errDenied {
			gcperrors.PermissionDenied(w, "")
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	logName := "projects/" + project + "/logs/" + logID
	if _, err := s.Store.DeleteLogEntries(project, logName); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Service) listLogs(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if project == "" {
		gcperrors.InvalidArgument(w, "project is required")
		return
	}
	if err := s.require(p, "logging.logs.list", project); err != nil {
		if err == errDenied {
			gcperrors.PermissionDenied(w, "")
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	names, err := s.Store.ListLogNames(project)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	calNames, err := s.Store.ListCloudAuditLogNames(project)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	names = mergeUniqueSorted(names, calNames)
	if names == nil {
		names = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"logNames": names})
}

func mergeUniqueSorted(a, b []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(a)+len(b))
	for _, list := range [][]string{a, b} {
		for _, n := range list {
			if _, ok := seen[n]; ok {
				continue
			}
			seen[n] = struct{}{}
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

func (s *Service) queryEntries(projectID, filter string, pageSize, offset int) ([]store.LogEntry, error) {
	exactLog, textContains, severity, tsGTE, tsLT := parseFilter(filter)
	if wantsCloudAudit(exactLog, filter) {
		return s.Store.ListCloudAuditAsLogEntries(store.ListCloudAuditFilter{
			ProjectID: projectID, ExactLogName: exactLog,
			TimestampGTE: tsGTE, TimestampLT: tsLT,
			PageSize: pageSize, Offset: offset,
		})
	}
	return s.Store.ListLogEntries(store.ListLogEntriesFilter{
		ProjectID: projectID, ExactLogName: exactLog, TextPayloadContain: textContains,
		Severity: severity, TimestampGTE: tsGTE, TimestampLT: tsLT,
		PageSize: pageSize, Offset: offset,
	})
}

func entriesToMaps(entries []store.LogEntry) []map[string]any {
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
		if pp, ok := payload["protoPayload"]; ok {
			item["protoPayload"] = pp
		}
		if e.ResourceJSON != "" && e.ResourceJSON != "{}" {
			var res any
			if json.Unmarshal([]byte(e.ResourceJSON), &res) == nil {
				item["resource"] = res
			}
		}
		out = append(out, item)
	}
	return out
}

func projectFromListReq(req listReq) (string, error) {
	for _, rn := range req.ResourceNames {
		if strings.HasPrefix(rn, "projects/") {
			parts := strings.Split(rn, "/")
			if len(parts) >= 2 && parts[1] != "" {
				return parts[1], nil
			}
		}
	}
	if len(req.ProjectIds) > 0 && req.ProjectIds[0] != "" {
		return req.ProjectIds[0], nil
	}
	return "", fmt.Errorf("resourceNames or projectIds is required")
}

func parseFilter(filter string) (exactLogName, textContains, severity, timestampGTE, timestampLT string) {
	filter = strings.TrimSpace(filter)
	if filter == "" {
		return "", "", "", "", ""
	}
	if m := reLogNameExact.FindStringSubmatch(filter); len(m) == 2 {
		return m[1], "", "", "", ""
	}
	if m := reTextContains.FindStringSubmatch(filter); len(m) == 2 {
		return "", m[1], "", "", ""
	}
	if m := reLogNameEq.FindStringSubmatch(filter); len(m) == 2 {
		exactLogName = m[1]
	}
	if m := reTextColon.FindStringSubmatch(filter); len(m) == 2 {
		textContains = m[1]
	}
	if m := reSeverityExact.FindStringSubmatch(filter); len(m) == 2 {
		severity = strings.ToUpper(m[1])
	}
	if m := reTimestampGTE.FindStringSubmatch(filter); len(m) == 2 {
		timestampGTE = m[1]
	} else if m := reTimestampGT.FindStringSubmatch(filter); len(m) == 2 {
		timestampGTE = m[1]
	}
	if m := reTimestampLT.FindStringSubmatch(filter); len(m) == 2 {
		timestampLT = m[1]
	} else if m := reTimestampLTE.FindStringSubmatch(filter); len(m) == 2 {
		// Lab lite: <= uses the same exclusive upper bound as < (equal timestamps excluded).
		timestampLT = m[1]
	}
	return exactLogName, textContains, severity, timestampGTE, timestampLT
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

func writeAuthz(w http.ResponseWriter, err error) {
	if err == errDenied {
		gcperrors.PermissionDenied(w, "")
		return
	}
	gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
}
