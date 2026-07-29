package cloudsql

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/compute"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/restlab"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// DefaultRegion is the lab default Cloud SQL region.
const DefaultRegion = "us-central1"

const (
	imagePostgres = "postgres:16-alpine"
	imageMySQL    = "mysql:8.0"
)

// NestedLabPassword is the fixed root/postgres password for nested SQL containers.
const NestedLabPassword = "noctaxris-gcp-lab"

// LabDaemon is the nested DinD surface used by Cloud SQL (tests inject stubs).
type LabDaemon interface {
	Enabled() bool
	StartLabDaemon(ctx context.Context, imageRef, containerName string, env []string, port int) (compute.LabDaemonResult, error)
	RemoveLabDaemon(ctx context.Context, containerID string) error
	ExecLabDaemon(ctx context.Context, containerID string, cmd []string) error
}

// Service serves Cloud SQL Admin REST v1 (instances CRUD; optional nested engine).
type Service struct {
	Store   *store.Store
	Authz   *authz.Evaluator
	Compute LabDaemon
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// Mount registers Cloud SQL Admin routes under /sql/v1/ and /sql/v1beta4/
// (avoids Spanner /v1/projects/.../instances; v1beta4 is the TF provider alias).
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	s.mountAPI(mux, principalFrom, "/sql/v1")
	s.mountV1Beta4(mux, principalFrom)
}

func (s *Service) mountAPI(mux *http.ServeMux, principalFrom principalFunc, prefix string) {
	mux.HandleFunc("GET "+prefix+"/projects/{project}/instances", s.wrap(principalFrom, s.listInstances))
	mux.HandleFunc("POST "+prefix+"/projects/{project}/instances", s.wrap(principalFrom, s.createInstance))
	mux.HandleFunc("GET "+prefix+"/projects/{project}/instances/{instance}", s.wrap(principalFrom, s.getInstance))
	mux.HandleFunc("DELETE "+prefix+"/projects/{project}/instances/{instance}", s.wrap(principalFrom, s.deleteInstance))
	s.mountUsers(mux, principalFrom, prefix)
	s.mountDatabases(mux, principalFrom, prefix)
	s.mountOperations(mux, principalFrom, prefix)
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

func (s *Service) createInstance(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "cloudsql.instances.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	if !restlab.RequireServiceEnabled(w, s.Store, project, "sqladmin.googleapis.com") {
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	instanceID := strings.TrimSpace(r.URL.Query().Get("instanceId"))
	if instanceID == "" {
		if n, ok := body["name"].(string); ok && n != "" {
			parts := strings.Split(n, ":")
			instanceID = parts[len(parts)-1]
		}
	}
	if instanceID == "" {
		gcperrors.InvalidArgument(w, "instanceId query parameter or name field is required")
		return
	}
	dbVersion, _ := body["databaseVersion"].(string)
	dbVersion = normalizeDatabaseVersion(dbVersion)
	if dbVersion == "" {
		gcperrors.InvalidArgument(w, "databaseVersion must be POSTGRES_* or MYSQL_*")
		return
	}
	region, _ := body["region"].(string)
	if region == "" {
		region = DefaultRegion
	}
	tier := "db-f1-micro"
	if settings, ok := body["settings"].(map[string]any); ok {
		if t, ok := settings["tier"].(string); ok && t != "" {
			tier = t
		}
	}
	settingsJSON := "{}"
	if settings, ok := body["settings"]; ok {
		raw, _ := json.Marshal(settings)
		settingsJSON = string(raw)
	}
	labelsJSON := "{}"
	if labels, ok := body["labels"]; ok {
		raw, _ := json.Marshal(labels)
		labelsJSON = string(raw)
	}

	name := store.CloudSQLInstanceResourceName(project, instanceID)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	inst := store.CloudSQLInstance{
		Name: name, ProjectID: project, InstanceID: instanceID, Region: region,
		DatabaseVersion: dbVersion, State: "RUNNABLE", Tier: tier,
		SettingsJSON: settingsJSON, LabelsJSON: labelsJSON, CreatedAt: now,
	}
	created, err := s.Store.CreateCloudSQLInstance(inst)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "instance already exists")
		return
	}

	if err := s.tryStartNested(r.Context(), name, instanceID, dbVersion); err != nil {
		_, _, _ = s.Store.DeleteCloudSQLInstance(name)
		gcperrors.WriteREST(w, http.StatusBadRequest, gcperrors.StatusFailedPrecondition,
			compute.NestedEngineFailClosedMessage(err))
		return
	}

	if _, ok, err := s.Store.GetCloudSQLInstance(name); err != nil || !ok {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "created instance missing")
		return
	}
	writeJSON(w, http.StatusOK, sqlOperationJSON("CREATE", instanceID, project))
}

func (s *Service) getInstance(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	id := r.PathValue("instance")
	if err := s.require(p, "cloudsql.instances.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	inst, ok, err := s.Store.GetCloudSQLInstance(store.CloudSQLInstanceResourceName(project, id))
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
	if err := s.require(p, "cloudsql.instances.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	list, err := s.Store.ListCloudSQLInstances(project)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, inst := range list {
		items = append(items, toInstanceJSON(inst))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Service) deleteInstance(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	id := r.PathValue("instance")
	if err := s.require(p, "cloudsql.instances.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := store.CloudSQLInstanceResourceName(project, id)
	ok, containerID, err := s.Store.DeleteCloudSQLInstance(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Instance not found")
		return
	}
	if s.Compute != nil && s.Compute.Enabled() && containerID != "" {
		_ = s.Compute.RemoveLabDaemon(r.Context(), containerID)
	}
	writeJSON(w, http.StatusOK, sqlOperationJSON("DELETE", id, project))
}

func (s *Service) tryStartNested(ctx context.Context, resourceName, instanceID, databaseVersion string) error {
	if s.Compute == nil || !s.Compute.Enabled() {
		return nil
	}
	image, env, port := nestedImageEnv(databaseVersion)
	if image == "" {
		return nil
	}
	containerName := "noctaxris-gcp-sql-" + sanitizeContainerSuffix(instanceID)
	out, err := s.Compute.StartLabDaemon(ctx, image, containerName, env, port)
	if err != nil {
		if compute.NestedEngineFailClosed() {
			return err
		}
		return nil
	}
	_ = s.Store.UpdateCloudSQLInstanceNested(resourceName, out.Host, out.Port, out.ContainerID)
	return nil
}

func nestedImageEnv(databaseVersion string) (image string, env []string, port int) {
	v := strings.ToUpper(databaseVersion)
	switch {
	case strings.HasPrefix(v, "MYSQL"):
		return imageMySQL, []string{"MYSQL_ROOT_PASSWORD=" + NestedLabPassword}, 3306
	case strings.HasPrefix(v, "POSTGRES"):
		return imagePostgres, []string{"POSTGRES_PASSWORD=" + NestedLabPassword}, 5432
	default:
		return "", nil, 0
	}
}

func isMySQLVersion(databaseVersion string) bool {
	return strings.HasPrefix(strings.ToUpper(databaseVersion), "MYSQL")
}

func quoteSQLLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func quoteSQLIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// safeSQLName allows lab identifiers used in nested CREATE USER/DATABASE.
func safeSQLName(name string) bool {
	if name == "" || len(name) > 63 {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func (s *Service) softExecNested(ctx context.Context, containerID string, cmd []string) {
	if s.Compute == nil || !s.Compute.Enabled() || strings.TrimSpace(containerID) == "" || len(cmd) == 0 {
		return
	}
	_ = s.Compute.ExecLabDaemon(ctx, containerID, cmd)
}

func sanitizeContainerSuffix(id string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(id) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else if r == '_' {
			b.WriteRune('-')
		}
	}
	s := b.String()
	if s == "" {
		return "inst"
	}
	if len(s) > 40 {
		return s[:40]
	}
	return s
}

func normalizeDatabaseVersion(v string) string {
	v = strings.TrimSpace(strings.ToUpper(v))
	switch v {
	case "POSTGRES", "POSTGRESQL":
		return "POSTGRES_16"
	case "MYSQL":
		return "MYSQL_8_0"
	case "POSTGRES_16", "POSTGRES_15", "POSTGRES_14", "MYSQL_8_0", "MYSQL_5_7":
		return v
	default:
		if strings.HasPrefix(v, "POSTGRES_") || strings.HasPrefix(v, "MYSQL_") {
			return v
		}
		return ""
	}
}

func toInstanceJSON(inst store.CloudSQLInstance) map[string]any {
	var settings any = map[string]any{}
	_ = json.Unmarshal([]byte(inst.SettingsJSON), &settings)
	if settings == nil {
		settings = map[string]any{}
	}
	settingsMap, _ := settings.(map[string]any)
	if settingsMap == nil {
		settingsMap = map[string]any{}
	}
	if _, ok := settingsMap["tier"]; !ok && inst.Tier != "" {
		settingsMap["tier"] = inst.Tier
	}
	var labels any = map[string]string{}
	_ = json.Unmarshal([]byte(inst.LabelsJSON), &labels)

	ipType := "PRIMARY"
	return map[string]any{
		"kind":            "sql#instance",
		"name":            inst.ConnectionName,
		"project":         inst.ProjectID,
		"databaseVersion": inst.DatabaseVersion,
		"region":          inst.Region,
		"state":           inst.State,
		"connectionName":  inst.ConnectionName,
		"settings":        settingsMap,
		"labels":          labels,
		"createTime":      inst.CreatedAt,
		"ipAddresses": []map[string]any{
			{"type": ipType, "ipAddress": inst.IPAddress},
		},
		"serverCaCert": map[string]any{
			"kind": "sql#sslCert",
		},
		"backendType": "SECOND_GEN",
		"instanceType": "CLOUD_SQL_INSTANCE",
		"selfLink": fmt.Sprintf("https://sqladmin.googleapis.com/sql/v1/projects/%s/instances/%s",
			inst.ProjectID, inst.InstanceID),
		"gceZone": inst.Region + "-a",
		"host":    inst.Host,
		"port":    inst.Port,
	}
}
