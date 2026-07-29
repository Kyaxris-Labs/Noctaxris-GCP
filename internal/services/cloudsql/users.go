package cloudsql

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func (s *Service) mountUsers(mux *http.ServeMux, principalFrom principalFunc, prefix string) {
	mux.HandleFunc("GET "+prefix+"/projects/{project}/instances/{instance}/users", s.wrap(principalFrom, s.listUsers))
	mux.HandleFunc("POST "+prefix+"/projects/{project}/instances/{instance}/users", s.wrap(principalFrom, s.createUser))
	mux.HandleFunc("GET "+prefix+"/projects/{project}/instances/{instance}/users/{name}", s.wrap(principalFrom, s.getUser))
	mux.HandleFunc("DELETE "+prefix+"/projects/{project}/instances/{instance}/users", s.wrap(principalFrom, s.deleteUser))
}

func (s *Service) createUser(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	instanceID := r.PathValue("instance")
	if err := s.require(p, "cloudsql.users.create", project); err != nil {
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
	host, _ := body["host"].(string)
	password, _ := body["password"].(string)
	userType, _ := body["type"].(string)
	if userType == "" {
		userType = "BUILT_IN"
	}
	if userType == "BUILT_IN" && password == "" {
		gcperrors.InvalidArgument(w, "password is required for BUILT_IN users")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	created, err := s.Store.CreateCloudSQLUser(store.CloudSQLUser{
		InstanceName: instName, ProjectID: project, InstanceID: instanceID,
		Name: name, Host: host, Password: password, Type: userType, CreatedAt: now,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "user already exists")
		return
	}
	s.softExecNested(r.Context(), inst.ContainerID, nestedCreateUserCmd(inst.DatabaseVersion, name, host, password))
	writeJSON(w, http.StatusOK, sqlOperationJSON("CREATE_USER", instanceID, project))
}

func (s *Service) getUser(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	instanceID := r.PathValue("instance")
	name := r.PathValue("name")
	host := r.URL.Query().Get("host")
	if err := s.require(p, "cloudsql.users.get", project); err != nil {
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
	u, ok, err := s.Store.GetCloudSQLUser(instName, name, host)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "User not found")
		return
	}
	writeJSON(w, http.StatusOK, toUserJSON(u))
}

func (s *Service) listUsers(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	instanceID := r.PathValue("instance")
	if err := s.require(p, "cloudsql.users.list", project); err != nil {
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
	list, err := s.Store.ListCloudSQLUsers(instName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, u := range list {
		items = append(items, toUserJSON(u))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"kind":  "sql#usersList",
		"items": items,
	})
}

func (s *Service) deleteUser(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	instanceID := r.PathValue("instance")
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	host := r.URL.Query().Get("host")
	if err := s.require(p, "cloudsql.users.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	if name == "" {
		gcperrors.InvalidArgument(w, "name query parameter is required")
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
	deleted, err := s.Store.DeleteCloudSQLUser(instName, name, host)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !deleted {
		gcperrors.NotFound(w, "User not found")
		return
	}
	s.softExecNested(r.Context(), inst.ContainerID, nestedDropUserCmd(inst.DatabaseVersion, name, host))
	writeJSON(w, http.StatusOK, sqlOperationJSON("DELETE_USER", instanceID, project))
}

func toUserJSON(u store.CloudSQLUser) map[string]any {
	return map[string]any{
		"kind":     "sql#user",
		"name":     u.Name,
		"host":     u.Host,
		"instance": u.InstanceID,
		"project":  u.ProjectID,
		"type":     u.Type,
	}
}

func safeMySQLHost(host string) bool {
	if host == "" || host == "%" {
		return true
	}
	for _, r := range host {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '_' || r == '-' || r == '.' || r == '%' {
			continue
		}
		return false
	}
	return true
}

func nestedCreateUserCmd(databaseVersion, name, host, password string) []string {
	if !safeSQLName(name) {
		return nil
	}
	if isMySQLVersion(databaseVersion) {
		h := host
		if h == "" {
			h = "%"
		}
		if !safeMySQLHost(h) {
			return nil
		}
		sql := "CREATE USER " + quoteSQLLiteral(name) + "@" + quoteSQLLiteral(h) +
			" IDENTIFIED BY " + quoteSQLLiteral(password)
		return []string{"mysql", "-uroot", "-p" + NestedLabPassword, "-e", sql}
	}
	sql := "CREATE USER " + quoteSQLIdent(name) + " WITH PASSWORD " + quoteSQLLiteral(password)
	return []string{"psql", "-U", "postgres", "-c", sql}
}

func nestedDropUserCmd(databaseVersion, name, host string) []string {
	if !safeSQLName(name) {
		return nil
	}
	if isMySQLVersion(databaseVersion) {
		h := host
		if h == "" {
			h = "%"
		}
		if !safeMySQLHost(h) {
			return nil
		}
		sql := "DROP USER IF EXISTS " + quoteSQLLiteral(name) + "@" + quoteSQLLiteral(h)
		return []string{"mysql", "-uroot", "-p" + NestedLabPassword, "-e", sql}
	}
	sql := "DROP USER IF EXISTS " + quoteSQLIdent(name)
	return []string{"psql", "-U", "postgres", "-c", sql}
}
