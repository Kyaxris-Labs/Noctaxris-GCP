package spanner

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// Service serves Spanner Admin REST v1 (instances/databases) plus ExecuteSql theatre.
// No Spanner server binary is embedded; SQL returns fixed empty/theatre rows.
type Service struct {
	Store *store.Store
	Authz *authz.Evaluator
}

type principalFunc func(*http.Request) (authn.Principal, bool)

var createDBNameRE = regexp.MustCompile(`(?i)CREATE\s+DATABASE\s+` + "`" + `?([A-Za-z0-9_-]+)` + "`" + `?`)

// Mount registers Spanner Admin and session ExecuteSql REST routes.
// Colon methods (:executeSql, :read, :partitionQuery, sessions:batchCreate) are
// parsed from path segments (ServeMux-safe).
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	mux.HandleFunc("GET /v1/projects/{project}/instanceConfigs", s.wrap(principalFrom, s.listInstanceConfigs))
	mux.HandleFunc("GET /v1/projects/{project}/instances", s.wrap(principalFrom, s.listInstances))
	mux.HandleFunc("POST /v1/projects/{project}/instances", s.wrap(principalFrom, s.createInstance))
	mux.HandleFunc("GET /v1/projects/{project}/instances/{instance}", s.wrap(principalFrom, s.getInstance))
	mux.HandleFunc("DELETE /v1/projects/{project}/instances/{instance}", s.wrap(principalFrom, s.deleteInstance))

	mux.HandleFunc("GET /v1/projects/{project}/instances/{instance}/databases", s.wrap(principalFrom, s.listDatabases))
	mux.HandleFunc("POST /v1/projects/{project}/instances/{instance}/databases", s.wrap(principalFrom, s.createDatabase))
	mux.HandleFunc("GET /v1/projects/{project}/instances/{instance}/databases/{database}", s.wrap(principalFrom, s.getDatabase))
	mux.HandleFunc("DELETE /v1/projects/{project}/instances/{instance}/databases/{database}", s.wrap(principalFrom, s.deleteDatabase))
	mux.HandleFunc("PATCH /v1/projects/{project}/instances/{instance}/databases/{database}/ddl", s.wrap(principalFrom, s.updateDDL))

	mux.HandleFunc("POST /v1/projects/{project}/instances/{instance}/databases/{database}/sessions", s.wrap(principalFrom, s.createSession))
	mux.HandleFunc("POST /v1/projects/{project}/instances/{instance}/databases/{database}/{sessionsCol}", s.wrap(principalFrom, s.sessionsCollectionPost))
	mux.HandleFunc("POST /v1/projects/{project}/instances/{instance}/databases/{database}/sessions/{session}", s.wrap(principalFrom, s.sessionAction))
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

func writeAuthzErr(w http.ResponseWriter, err error) {
	if err == errDenied {
		gcperrors.PermissionDenied(w, "")
		return
	}
	gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
}

func splitAction(seg string) (name, action string) {
	if i := strings.IndexByte(seg, ':'); i >= 0 {
		return seg[:i], seg[i+1:]
	}
	return seg, ""
}

func instanceName(project, id string) string {
	return fmt.Sprintf("projects/%s/instances/%s", project, id)
}

func databaseName(project, instance, id string) string {
	return fmt.Sprintf("projects/%s/instances/%s/databases/%s", project, instance, id)
}

func (s *Service) createInstance(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "spanner.instances.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	var body struct {
		InstanceID string         `json:"instanceId"`
		Instance   map[string]any `json:"instance"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	if body.InstanceID == "" {
		gcperrors.InvalidArgument(w, "instanceId is required")
		return
	}
	if body.Instance == nil {
		body.Instance = map[string]any{}
	}
	config, _ := body.Instance["config"].(string)
	displayName, _ := body.Instance["displayName"].(string)
	nodeCount := intFromAny(body.Instance["nodeCount"])
	processingUnits := intFromAny(body.Instance["processingUnits"])
	labelsJSON := "{}"
	if labels, ok := body.Instance["labels"]; ok {
		raw, _ := json.Marshal(labels)
		labelsJSON = string(raw)
	}
	name := instanceName(project, body.InstanceID)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	created, err := s.Store.CreateSpannerInstance(store.SpannerInstance{
		Name: name, ProjectID: project, InstanceID: body.InstanceID,
		Config: config, DisplayName: displayName, NodeCount: nodeCount, ProcessingUnits: processingUnits,
		State: "READY", LabelsJSON: labelsJSON, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "instance already exists")
		return
	}
	out, _, _ := s.Store.GetSpannerInstance(name)
	writeJSON(w, http.StatusOK, toInstanceJSON(out))
}

func (s *Service) getInstance(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	id, _ := splitAction(r.PathValue("instance"))
	if err := s.require(p, "spanner.instances.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	inst, ok, err := s.Store.GetSpannerInstance(instanceName(project, id))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Instance not found")
		return
	}
	writeJSON(w, http.StatusOK, toInstanceJSON(inst))
}

func (s *Service) listInstances(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "spanner.instances.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	list, err := s.Store.ListSpannerInstances(project)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, inst := range list {
		items = append(items, toInstanceJSON(inst))
	}
	writeJSON(w, http.StatusOK, map[string]any{"instances": items})
}

func (s *Service) deleteInstance(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	id, _ := splitAction(r.PathValue("instance"))
	if err := s.require(p, "spanner.instances.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	ok, err := s.Store.DeleteSpannerInstance(instanceName(project, id))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Instance not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Service) createDatabase(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	instID, _ := splitAction(r.PathValue("instance"))
	if err := s.require(p, "spanner.databases.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	instName := instanceName(project, instID)
	if _, ok, err := s.Store.GetSpannerInstance(instName); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "Instance not found")
		return
	}
	var body struct {
		CreateStatement string   `json:"createStatement"`
		ExtraStatements []string `json:"extraStatements"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	dbID := parseCreateDatabaseID(body.CreateStatement)
	if dbID == "" {
		gcperrors.InvalidArgument(w, "createStatement must be CREATE DATABASE `id`")
		return
	}
	extraJSON := "[]"
	if len(body.ExtraStatements) > 0 {
		raw, _ := json.Marshal(body.ExtraStatements)
		extraJSON = string(raw)
	}
	name := databaseName(project, instID, dbID)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	created, err := s.Store.CreateSpannerDatabase(store.SpannerDatabase{
		Name: name, InstanceName: instName, ProjectID: project, InstanceID: instID, DatabaseID: dbID,
		State: "READY", CreateStatement: body.CreateStatement, ExtraStatementsJSON: extraJSON, CreatedAt: now,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "database already exists")
		return
	}
	out, _, _ := s.Store.GetSpannerDatabase(name)
	writeJSON(w, http.StatusOK, toDatabaseJSON(out))
}

func (s *Service) getDatabase(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	instID, _ := splitAction(r.PathValue("instance"))
	dbID, _ := splitAction(r.PathValue("database"))
	if err := s.require(p, "spanner.databases.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	db, ok, err := s.Store.GetSpannerDatabase(databaseName(project, instID, dbID))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Database not found")
		return
	}
	writeJSON(w, http.StatusOK, toDatabaseJSON(db))
}

func (s *Service) listDatabases(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	instID, _ := splitAction(r.PathValue("instance"))
	if err := s.require(p, "spanner.databases.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	list, err := s.Store.ListSpannerDatabases(instanceName(project, instID))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, db := range list {
		items = append(items, toDatabaseJSON(db))
	}
	writeJSON(w, http.StatusOK, map[string]any{"databases": items})
}

func (s *Service) deleteDatabase(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	instID, _ := splitAction(r.PathValue("instance"))
	dbID, _ := splitAction(r.PathValue("database"))
	if err := s.require(p, "spanner.databases.drop", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	ok, err := s.Store.DeleteSpannerDatabase(databaseName(project, instID, dbID))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Database not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Service) createSession(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	instID, _ := splitAction(r.PathValue("instance"))
	dbID, _ := splitAction(r.PathValue("database"))
	if err := s.require(p, "spanner.sessions.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	dbName := databaseName(project, instID, dbID)
	if _, ok, err := s.Store.GetSpannerDatabase(dbName); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "Database not found")
		return
	}
	sess, created, err := s.Store.CreateSpannerSession(store.SpannerSession{
		DatabaseName: dbName, ProjectID: project, InstanceID: instID, DatabaseID: dbID,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "session already exists")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name":       sess.Name,
		"createTime": sess.CreatedAt,
	})
}

func (s *Service) sessionsCollectionPost(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	col, action := splitAction(r.PathValue("sessionsCol"))
	if col != "sessions" || action != "batchCreate" {
		gcperrors.NotFound(w, "unknown Spanner sessions collection method")
		return
	}
	s.batchCreateSessions(w, r, p)
}

func (s *Service) batchCreateSessions(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	instID, _ := splitAction(r.PathValue("instance"))
	dbID, _ := splitAction(r.PathValue("database"))
	if err := s.require(p, "spanner.sessions.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	dbName := databaseName(project, instID, dbID)
	if _, ok, err := s.Store.GetSpannerDatabase(dbName); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "Database not found")
		return
	}
	var body struct {
		SessionCount int `json:"sessionCount"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.SessionCount <= 0 {
		body.SessionCount = 1
	}
	if body.SessionCount > 100 {
		body.SessionCount = 100
	}
	sessions := make([]map[string]any, 0, body.SessionCount)
	for i := 0; i < body.SessionCount; i++ {
		sess, created, err := s.Store.CreateSpannerSession(store.SpannerSession{
			DatabaseName: dbName, ProjectID: project, InstanceID: instID, DatabaseID: dbID,
		})
		if err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		}
		if !created {
			continue
		}
		sessions = append(sessions, map[string]any{
			"name":       sess.Name,
			"createTime": sess.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": sessions})
}

func (s *Service) updateDDL(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	instID, _ := splitAction(r.PathValue("instance"))
	dbID, _ := splitAction(r.PathValue("database"))
	if err := s.require(p, "spanner.databases.updateDdl", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	dbName := databaseName(project, instID, dbID)
	var body struct {
		Statements  []string `json:"statements"`
		OperationID string   `json:"operationId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	if len(body.Statements) == 0 {
		gcperrors.InvalidArgument(w, "statements is required")
		return
	}
	db, ok, err := s.Store.AppendSpannerDDL(dbName, body.Statements)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Database not found")
		return
	}
	opID := body.OperationID
	if opID == "" {
		opID = fmt.Sprintf("_%d", time.Now().UnixNano())
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	// Theatre: completed Operation (no Spanner DDL engine).
	writeJSON(w, http.StatusOK, map[string]any{
		"name": db.Name + "/operations/" + opID,
		"metadata": map[string]any{
			"@type":      "type.googleapis.com/google.spanner.admin.database.v1.UpdateDatabaseDdlMetadata",
			"database":   db.Name,
			"statements": body.Statements,
		},
		"done": true,
		"response": map[string]any{
			"@type": "type.googleapis.com/google.protobuf.Empty",
		},
		"createTime": now,
		"endTime":    now,
	})
}

func (s *Service) listInstanceConfigs(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "spanner.instanceConfigs.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	cfgID := "regional-us-central1"
	writeJSON(w, http.StatusOK, map[string]any{
		"instanceConfigs": []map[string]any{
			{
				"name":        fmt.Sprintf("projects/%s/instanceConfigs/%s", project, cfgID),
				"displayName": cfgID,
				"configType":  "GOOGLE_MANAGED",
				"replicas": []map[string]any{
					{"location": "us-central1", "type": "READ_WRITE", "defaultLeaderLocation": true},
				},
			},
		},
	})
}

func (s *Service) sessionAction(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	instID, _ := splitAction(r.PathValue("instance"))
	dbID, _ := splitAction(r.PathValue("database"))
	sessID, action := splitAction(r.PathValue("session"))
	switch action {
	case "executeSql":
		s.executeSQL(w, r, p, project, instID, dbID, sessID)
	case "read":
		s.read(w, r, p, project, instID, dbID, sessID)
	case "partitionQuery":
		s.partitionQuery(w, r, p, project, instID, dbID, sessID)
	default:
		gcperrors.NotFound(w, "unknown Spanner session method")
	}
}

func (s *Service) requireSession(w http.ResponseWriter, project, instID, dbID, sessID string) bool {
	sessName := databaseName(project, instID, dbID) + "/sessions/" + sessID
	if _, ok, err := s.Store.GetSpannerSession(sessName); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return false
	} else if !ok {
		gcperrors.NotFound(w, "Session not found")
		return false
	}
	return true
}

func emptyResultSet() map[string]any {
	return map[string]any{
		"metadata": map[string]any{
			"rowType": map[string]any{"fields": []any{}},
		},
		"rows": []any{},
		"stats": map[string]any{
			"rowCountExact": "0",
			"queryPlan": map[string]any{
				"planNodes": []any{
					map[string]any{"displayName": "LabExecuteSqlTheatre", "kind": "RELATIONAL"},
				},
			},
		},
	}
}

func (s *Service) executeSQL(w http.ResponseWriter, r *http.Request, p authn.Principal, project, instID, dbID, sessID string) {
	if err := s.require(p, "spanner.databases.select", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	if !s.requireSession(w, project, instID, dbID, sessID) {
		return
	}
	var body struct {
		SQL string `json:"sql"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	// Theatre: no Spanner dialect / query engine; return empty ResultSet-shaped JSON.
	writeJSON(w, http.StatusOK, emptyResultSet())
}

func (s *Service) read(w http.ResponseWriter, r *http.Request, p authn.Principal, project, instID, dbID, sessID string) {
	if err := s.require(p, "spanner.databases.read", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	if !s.requireSession(w, project, instID, dbID, sessID) {
		return
	}
	_ = json.NewDecoder(r.Body).Decode(&map[string]any{})
	writeJSON(w, http.StatusOK, emptyResultSet())
}

func (s *Service) partitionQuery(w http.ResponseWriter, r *http.Request, p authn.Principal, project, instID, dbID, sessID string) {
	if err := s.require(p, "spanner.databases.partitionQuery", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	if !s.requireSession(w, project, instID, dbID, sessID) {
		return
	}
	var body struct {
		SQL string `json:"sql"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	writeJSON(w, http.StatusOK, map[string]any{
		"partitions": []map[string]any{
			{"partitionToken": "bGFiLXBhcnRpdGlvbi0w"}, // base64("lab-partition-0")
		},
		"transaction": map[string]any{
			"id":            "bGFiLXR4",
			"readTimestamp": now,
		},
	})
}

func toInstanceJSON(inst store.SpannerInstance) map[string]any {
	var labels any = map[string]string{}
	_ = json.Unmarshal([]byte(inst.LabelsJSON), &labels)
	out := map[string]any{
		"name":        inst.Name,
		"config":      inst.Config,
		"displayName": inst.DisplayName,
		"state":       inst.State,
		"labels":      labels,
		"createTime":  inst.CreatedAt,
		"updateTime":  inst.UpdatedAt,
	}
	if inst.ProcessingUnits > 0 {
		out["processingUnits"] = inst.ProcessingUnits
	} else {
		out["nodeCount"] = inst.NodeCount
	}
	return out
}

func toDatabaseJSON(db store.SpannerDatabase) map[string]any {
	var ddl any = []any{}
	_ = json.Unmarshal([]byte(db.DDLStatementsJSON), &ddl)
	return map[string]any{
		"name":            db.Name,
		"state":           db.State,
		"createTime":      db.CreatedAt,
		"databaseDialect": db.Dialect,
		"reconciling":     false,
		"ddlStatements":   ddl,
	}
}

func parseCreateDatabaseID(stmt string) string {
	m := createDBNameRE.FindStringSubmatch(strings.TrimSpace(stmt))
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func intFromAny(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	default:
		return 0
	}
}
