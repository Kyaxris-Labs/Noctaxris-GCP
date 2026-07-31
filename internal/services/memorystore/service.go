package memorystore

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
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
	"github.com/google/uuid"
)

// DefaultLocation is the lab default Memorystore region.
const DefaultLocation = "us-central1"

// NestEngine is the nested Redis surface (tests inject stubs).
// When nil, Dial from NOCTAXRIS_GCP_DOCKER_HOST is used.
type NestEngine interface {
	Enabled() bool
	EnsureMemorystoreRedis(ctx context.Context, instanceID, authPassword string) (compute.MemorystoreRedisResult, error)
	RemoveMemorystoreRedis(ctx context.Context, instanceID, containerID string) error
	Close() error
}

// Service serves Memorystore for Redis REST v1 (instances CRUD).
// Without NOCTAXRIS_GCP_DOCKER_HOST, host/port are theatre metadata only.
// With a nested engine configured, create attempts redis:7-alpine on the shared
// noctaxris-gcp-lab bridge (no host port publish).
type Service struct {
	Store  *store.Store
	Authz  *authz.Evaluator
	Engine NestEngine
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
	s.mountOperations(mux, principalFrom)
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
	authEnabled := boolFromAny(body["authEnabled"])
	authString, _ := body["authString"].(string)
	authString = strings.TrimSpace(authString)
	if authEnabled {
		if authString == "" {
			authString = uuid.NewString()
		}
	} else {
		authString = ""
	}
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
		State: "READY", LabelsJSON: labelsJSON, AuthorizedNetwork: authNet,
		AuthEnabled: authEnabled, AuthString: authString, CreatedAt: now,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "instance already exists")
		return
	}
	if err := s.tryNestedRedisOnCreate(r.Context(), name, instanceID, authString); err != nil {
		_, _ = s.Store.DeleteMemorystoreRedisInstance(name)
		gcperrors.WriteREST(w, http.StatusBadRequest, gcperrors.StatusFailedPrecondition,
			compute.NestedEngineFailClosedMessage(err))
		return
	}
	out, ok, err := s.Store.GetMemorystoreRedisInstance(name)
	if err != nil || !ok {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "created instance missing")
		return
	}
	resp := toInstanceJSON(out)
	resp["@type"] = instanceTypeURL
	writeDoneOperation(w, project, location, "create-"+instanceID, resp)
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
	fullName := instanceName(project, location, id)
	inst, found, err := s.Store.GetMemorystoreRedisInstance(fullName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !found {
		gcperrors.NotFound(w, "Instance not found")
		return
	}
	s.tryNestedRedisOnDelete(r.Context(), inst.InstanceID, inst.ContainerID)
	ok, err := s.Store.DeleteMemorystoreRedisInstance(fullName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Instance not found")
		return
	}
	// Done Operation so Terraform redis waiters complete on destroy (same shape as create).
	writeDoneOperation(w, project, location, "delete-"+id, nil)
}

func toInstanceJSON(inst store.MemorystoreRedisInstance) map[string]any {
	var labels any = map[string]string{}
	_ = json.Unmarshal([]byte(inst.LabelsJSON), &labels)
	out := map[string]any{
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
		"authEnabled":       inst.AuthEnabled,
		"createTime":        inst.CreatedAt,
		"currentLocationId": inst.Location + "-a",
		"locationId":        inst.Location + "-a",
	}
	// Lab convenience: echo authString on Instance get/create (GCP uses getAuthString).
	if inst.AuthEnabled && inst.AuthString != "" {
		out["authString"] = inst.AuthString
	}
	return out
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

func boolFromAny(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return strings.EqualFold(b, "true") || b == "1"
	default:
		return false
	}
}
