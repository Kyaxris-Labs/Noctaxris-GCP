package memorystore

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

// DefaultLocation is the lab default Memorystore region.
const DefaultLocation = "us-central1"

// Service serves Memorystore for Redis REST v1 (instances CRUD theatre).
// No Redis process is started; host/port are theatre fields only.
type Service struct {
	Store *store.Store
	Authz *authz.Evaluator
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// Mount registers location-scoped Redis instance routes.
// Paths use /v1/projects/{p}/locations/{loc}/instances — never bare /v1/projects/{p}/instances
// (Spanner owns that shape).
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/instances", s.wrap(principalFrom, s.listInstances))
	mux.HandleFunc("POST /v1/projects/{project}/locations/{location}/instances", s.wrap(principalFrom, s.createInstance))
	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/instances/{instance}", s.wrap(principalFrom, s.getInstance))
	mux.HandleFunc("DELETE /v1/projects/{project}/locations/{location}/instances/{instance}", s.wrap(principalFrom, s.deleteInstance))
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

func instanceName(project, location, id string) string {
	return fmt.Sprintf("projects/%s/locations/%s/instances/%s", project, location, id)
}

func (s *Service) createInstance(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	if err := s.require(p, "redis.instances.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	instanceID := r.URL.Query().Get("instanceId")
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	if instanceID == "" {
		if id, ok := body["name"].(string); ok && id != "" {
			// Accept full name suffix or bare id in name for lab clients.
			parts := strings.Split(id, "/")
			instanceID = parts[len(parts)-1]
		}
	}
	if instanceID == "" {
		gcperrors.InvalidArgument(w, "instanceId query parameter is required")
		return
	}
	displayName, _ := body["displayName"].(string)
	tier, _ := body["tier"].(string)
	if tier == "" {
		tier = "BASIC"
	}
	redisVersion, _ := body["redisVersion"].(string)
	authNet, _ := body["authorizedNetwork"].(string)
	memGB := intFromAny(body["memorySizeGb"])
	labelsJSON := "{}"
	if labels, ok := body["labels"]; ok {
		raw, _ := json.Marshal(labels)
		labelsJSON = string(raw)
	}
	name := instanceName(project, location, instanceID)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	created, err := s.Store.CreateMemorystoreRedisInstance(store.MemorystoreRedisInstance{
		Name: name, ProjectID: project, Location: location, InstanceID: instanceID,
		DisplayName: displayName, Tier: tier, MemorySizeGb: memGB, RedisVersion: redisVersion,
		State: "READY", LabelsJSON: labelsJSON, AuthorizedNetwork: authNet, CreatedAt: now,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "instance already exists")
		return
	}
	out, _, _ := s.Store.GetMemorystoreRedisInstance(name)
	writeJSON(w, http.StatusOK, toInstanceJSON(out))
}

func (s *Service) getInstance(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	id, _ := splitAction(r.PathValue("instance"))
	if err := s.require(p, "redis.instances.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	inst, ok, err := s.Store.GetMemorystoreRedisInstance(instanceName(project, location, id))
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
	location := r.PathValue("location")
	if err := s.require(p, "redis.instances.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	list, err := s.Store.ListMemorystoreRedisInstances(project, location)
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
	location := r.PathValue("location")
	id, _ := splitAction(r.PathValue("instance"))
	if err := s.require(p, "redis.instances.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	ok, err := s.Store.DeleteMemorystoreRedisInstance(instanceName(project, location, id))
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

func toInstanceJSON(inst store.MemorystoreRedisInstance) map[string]any {
	var labels any = map[string]string{}
	_ = json.Unmarshal([]byte(inst.LabelsJSON), &labels)
	return map[string]any{
		"name":              inst.Name,
		"displayName":       inst.DisplayName,
		"tier":              inst.Tier,
		"memorySizeGb":      inst.MemorySizeGb,
		"redisVersion":      inst.RedisVersion,
		"host":              inst.Host,
		"port":              inst.Port,
		"state":             inst.State,
		"labels":            labels,
		"authorizedNetwork": inst.AuthorizedNetwork,
		"createTime":        inst.CreatedAt,
		"currentLocationId": inst.Location + "-a",
		"locationId":        inst.Location + "-a",
	}
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
