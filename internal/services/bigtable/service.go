package bigtable

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// Service serves Bigtable Admin REST v2 (instances/tables control-plane theatre).
// No Bigtable server binary; no row reads/writes.
type Service struct {
	Store *store.Store
	Authz *authz.Evaluator
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// Mount registers Bigtable Admin API v2 routes under /v2/...
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	mux.HandleFunc("GET /v2/projects/{project}/instances", s.wrap(principalFrom, s.listInstances))
	mux.HandleFunc("POST /v2/projects/{project}/instances", s.wrap(principalFrom, s.createInstance))
	mux.HandleFunc("GET /v2/projects/{project}/instances/{instance}", s.wrap(principalFrom, s.getInstance))
	mux.HandleFunc("DELETE /v2/projects/{project}/instances/{instance}", s.wrap(principalFrom, s.deleteInstance))

	mux.HandleFunc("GET /v2/projects/{project}/instances/{instance}/tables", s.wrap(principalFrom, s.listTables))
	mux.HandleFunc("POST /v2/projects/{project}/instances/{instance}/tables", s.wrap(principalFrom, s.createTable))
	mux.HandleFunc("GET /v2/projects/{project}/instances/{instance}/tables/{table}", s.wrap(principalFrom, s.getTable))
	mux.HandleFunc("DELETE /v2/projects/{project}/instances/{instance}/tables/{table}", s.wrap(principalFrom, s.deleteTable))
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

func tableName(project, instance, id string) string {
	return fmt.Sprintf("projects/%s/instances/%s/tables/%s", project, instance, id)
}

func (s *Service) createInstance(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "bigtable.instances.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	var body struct {
		InstanceID string         `json:"instanceId"`
		Instance   map[string]any `json:"instance"`
		Clusters   map[string]any `json:"clusters"`
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
	displayName, _ := body.Instance["displayName"].(string)
	instType, _ := body.Instance["type"].(string)
	labelsJSON := "{}"
	if labels, ok := body.Instance["labels"]; ok {
		raw, _ := json.Marshal(labels)
		labelsJSON = string(raw)
	}
	clustersJSON := "{}"
	if body.Clusters != nil {
		raw, _ := json.Marshal(body.Clusters)
		clustersJSON = string(raw)
	}
	name := instanceName(project, body.InstanceID)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	created, err := s.Store.CreateBigtableInstance(store.BigtableInstance{
		Name: name, ProjectID: project, InstanceID: body.InstanceID,
		DisplayName: displayName, State: "READY", Type: instType,
		LabelsJSON: labelsJSON, ClustersJSON: clustersJSON, CreatedAt: now,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "instance already exists")
		return
	}
	out, _, _ := s.Store.GetBigtableInstance(name)
	writeJSON(w, http.StatusOK, toInstanceJSON(out))
}

func (s *Service) getInstance(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	id, _ := splitAction(r.PathValue("instance"))
	if err := s.require(p, "bigtable.instances.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	inst, ok, err := s.Store.GetBigtableInstance(instanceName(project, id))
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
	if err := s.require(p, "bigtable.instances.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	list, err := s.Store.ListBigtableInstances(project)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, inst := range list {
		items = append(items, toInstanceJSON(inst))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"instances":       items,
		"failedLocations": []any{},
	})
}

func (s *Service) deleteInstance(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	id, _ := splitAction(r.PathValue("instance"))
	if err := s.require(p, "bigtable.instances.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	ok, err := s.Store.DeleteBigtableInstance(instanceName(project, id))
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

func (s *Service) createTable(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	instID, _ := splitAction(r.PathValue("instance"))
	if err := s.require(p, "bigtable.tables.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	instName := instanceName(project, instID)
	if _, ok, err := s.Store.GetBigtableInstance(instName); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "Instance not found")
		return
	}
	var body struct {
		TableID string         `json:"tableId"`
		Table   map[string]any `json:"table"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	if body.TableID == "" {
		gcperrors.InvalidArgument(w, "tableId is required")
		return
	}
	if body.Table == nil {
		body.Table = map[string]any{}
	}
	cfJSON := "{}"
	if cf, ok := body.Table["columnFamilies"]; ok {
		raw, _ := json.Marshal(cf)
		cfJSON = string(raw)
	}
	granularity, _ := body.Table["granularity"].(string)
	name := tableName(project, instID, body.TableID)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	created, err := s.Store.CreateBigtableTable(store.BigtableTable{
		Name: name, InstanceName: instName, ProjectID: project, InstanceID: instID, TableID: body.TableID,
		ColumnFamiliesJSON: cfJSON, Granularity: granularity, CreatedAt: now,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "table already exists")
		return
	}
	out, _, _ := s.Store.GetBigtableTable(name)
	writeJSON(w, http.StatusOK, toTableJSON(out))
}

func (s *Service) getTable(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	instID, _ := splitAction(r.PathValue("instance"))
	tblID, _ := splitAction(r.PathValue("table"))
	if err := s.require(p, "bigtable.tables.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	tbl, ok, err := s.Store.GetBigtableTable(tableName(project, instID, tblID))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Table not found")
		return
	}
	writeJSON(w, http.StatusOK, toTableJSON(tbl))
}

func (s *Service) listTables(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	instID, _ := splitAction(r.PathValue("instance"))
	if err := s.require(p, "bigtable.tables.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	list, err := s.Store.ListBigtableTables(instanceName(project, instID))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, tbl := range list {
		items = append(items, toTableJSON(tbl))
	}
	writeJSON(w, http.StatusOK, map[string]any{"tables": items})
}

func (s *Service) deleteTable(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	instID, _ := splitAction(r.PathValue("instance"))
	tblID, _ := splitAction(r.PathValue("table"))
	if err := s.require(p, "bigtable.tables.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	ok, err := s.Store.DeleteBigtableTable(tableName(project, instID, tblID))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Table not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func toInstanceJSON(inst store.BigtableInstance) map[string]any {
	var labels any = map[string]string{}
	_ = json.Unmarshal([]byte(inst.LabelsJSON), &labels)
	return map[string]any{
		"name":        inst.Name,
		"displayName": inst.DisplayName,
		"state":       inst.State,
		"type":        inst.Type,
		"labels":      labels,
		"createTime":  inst.CreatedAt,
	}
}

func toTableJSON(t store.BigtableTable) map[string]any {
	var cf any = map[string]any{}
	_ = json.Unmarshal([]byte(t.ColumnFamiliesJSON), &cf)
	return map[string]any{
		"name":           t.Name,
		"columnFamilies": cf,
		"granularity":    t.Granularity,
	}
}
