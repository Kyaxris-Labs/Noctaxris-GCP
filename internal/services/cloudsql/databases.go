package cloudsql

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

func (s *Service) mountDatabases(mux *http.ServeMux, principalFrom principalFunc, prefix string) {
	mux.HandleFunc("GET "+prefix+"/projects/{project}/instances/{instance}/databases", s.wrap(principalFrom, s.listDatabases))
	mux.HandleFunc("POST "+prefix+"/projects/{project}/instances/{instance}/databases", s.wrap(principalFrom, s.createDatabase))
	mux.HandleFunc("GET "+prefix+"/projects/{project}/instances/{instance}/databases/{database}", s.wrap(principalFrom, s.getDatabase))
	mux.HandleFunc("DELETE "+prefix+"/projects/{project}/instances/{instance}/databases/{database}", s.wrap(principalFrom, s.deleteDatabase))
}

func (s *Service) createDatabase(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	instanceID := r.PathValue("instance")
	if err := s.require(p, "cloudsql.databases.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	instName := store.CloudSQLInstanceResourceName(project, instanceID)
	inst, ok, err := s.Store.GetCloudSQLInstance(instName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Instance not found")
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	name, _ := body["name"].(string)
	name = strings.TrimSpace(name)
	if name == "" {
		gcperrors.InvalidArgument(w, "name is required")
		return
	}
	if !safeSQLName(name) {
		gcperrors.InvalidArgument(w, "name must be a simple SQL identifier")
		return
	}
	charset, _ := body["charset"].(string)
	collation, _ := body["collation"].(string)
	if charset == "" || collation == "" {
		defCharset, defCollation := defaultCharsetCollation(inst.DatabaseVersion)
		if charset == "" {
			charset = defCharset
		}
		if collation == "" {
			collation = defCollation
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	created, err := s.Store.CreateCloudSQLDatabase(store.CloudSQLDatabase{
		InstanceName: instName, ProjectID: project, InstanceID: instanceID,
		Name: name, Charset: charset, Collation: collation, CreatedAt: now,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "database already exists")
		return
	}
	s.softExecNested(r.Context(), inst.ContainerID, nestedCreateDatabaseCmd(inst.DatabaseVersion, name, charset, collation))
	writeJSON(w, http.StatusOK, sqlOperationJSON("CREATE_DATABASE", instanceID, project))
}

func (s *Service) getDatabase(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	instanceID := r.PathValue("instance")
	dbName := r.PathValue("database")
	if err := s.require(p, "cloudsql.databases.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	instName := store.CloudSQLInstanceResourceName(project, instanceID)
	if _, ok, err := s.Store.GetCloudSQLInstance(instName); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "Instance not found")
		return
	}
	d, ok, err := s.Store.GetCloudSQLDatabase(instName, dbName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Database not found")
		return
	}
	writeJSON(w, http.StatusOK, toDatabaseJSON(d))
}

func (s *Service) listDatabases(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	instanceID := r.PathValue("instance")
	if err := s.require(p, "cloudsql.databases.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	instName := store.CloudSQLInstanceResourceName(project, instanceID)
	if _, ok, err := s.Store.GetCloudSQLInstance(instName); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "Instance not found")
		return
	}
	list, err := s.Store.ListCloudSQLDatabases(instName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, d := range list {
		items = append(items, toDatabaseJSON(d))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"kind":  "sql#databasesList",
		"items": items,
	})
}

func (s *Service) deleteDatabase(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	instanceID := r.PathValue("instance")
	dbName := r.PathValue("database")
	if err := s.require(p, "cloudsql.databases.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	instName := store.CloudSQLInstanceResourceName(project, instanceID)
	inst, ok, err := s.Store.GetCloudSQLInstance(instName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Instance not found")
		return
	}
	deleted, err := s.Store.DeleteCloudSQLDatabase(instName, dbName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !deleted {
		gcperrors.NotFound(w, "Database not found")
		return
	}
	s.softExecNested(r.Context(), inst.ContainerID, nestedDropDatabaseCmd(inst.DatabaseVersion, dbName))
	writeJSON(w, http.StatusOK, sqlOperationJSON("DELETE_DATABASE", instanceID, project))
}

func toDatabaseJSON(d store.CloudSQLDatabase) map[string]any {
	return map[string]any{
		"kind":      "sql#database",
		"name":      d.Name,
		"charset":   d.Charset,
		"collation": d.Collation,
		"instance":  d.InstanceID,
		"project":   d.ProjectID,
		"selfLink": fmt.Sprintf("https://sqladmin.googleapis.com/sql/v1/projects/%s/instances/%s/databases/%s",
			d.ProjectID, d.InstanceID, d.Name),
	}
}

func defaultCharsetCollation(databaseVersion string) (charset, collation string) {
	if isMySQLVersion(databaseVersion) {
		return "utf8mb4", "utf8mb4_unicode_ci"
	}
	return "UTF8", "en_US.UTF8"
}

func nestedCreateDatabaseCmd(databaseVersion, name, charset, collation string) []string {
	if !safeSQLName(name) {
		return nil
	}
	if isMySQLVersion(databaseVersion) {
		sql := "CREATE DATABASE `" + strings.ReplaceAll(name, "`", "") + "`"
		if charset != "" && safeSQLName(charset) {
			sql += " CHARACTER SET " + charset
		}
		if collation != "" && safeSQLName(collation) {
			sql += " COLLATE " + collation
		}
		return []string{"mysql", "-uroot", "-p" + NestedLabPassword, "-e", sql}
	}
	sql := "CREATE DATABASE " + quoteSQLIdent(name)
	return []string{"psql", "-U", "postgres", "-c", sql}
}

func nestedDropDatabaseCmd(databaseVersion, name string) []string {
	if !safeSQLName(name) {
		return nil
	}
	if isMySQLVersion(databaseVersion) {
		sql := "DROP DATABASE IF EXISTS `" + strings.ReplaceAll(name, "`", "") + "`"
		return []string{"mysql", "-uroot", "-p" + NestedLabPassword, "-e", sql}
	}
	sql := "DROP DATABASE IF EXISTS " + quoteSQLIdent(name)
	return []string{"psql", "-U", "postgres", "-c", sql}
}
