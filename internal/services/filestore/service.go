package filestore

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/restlab"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// DefaultLocation is the lab default Filestore zone.
const DefaultLocation = "us-central1-a"

// Service serves Cloud Filestore REST v1 (instances CRUD theatre).
// No NFS server is started; fileShares and networks are metadata only.
//
// Paths use /file/v1/... so they do not collide with Memorystore Redis on
// /v1/projects/{p}/locations/{loc}/instances (identical ServeMux pattern).
type Service struct {
	Store *store.Store
	Authz *authz.Evaluator
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// Mount registers Filestore instance routes under the /file/v1/ path prefix.
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	mux.HandleFunc("GET /file/v1/projects/{project}/locations/{location}/instances", restlab.Wrap(principalFrom, s.listInstances))
	mux.HandleFunc("POST /file/v1/projects/{project}/locations/{location}/instances", restlab.Wrap(principalFrom, s.createInstance))
	mux.HandleFunc("GET /file/v1/projects/{project}/locations/{location}/instances/{instance}", restlab.Wrap(principalFrom, s.getInstance))
	mux.HandleFunc("DELETE /file/v1/projects/{project}/locations/{location}/instances/{instance}", restlab.Wrap(principalFrom, s.deleteInstance))

	// Lab Operations.get: create returns done:true; poll path succeeds immediately for TF waiters.
	mux.HandleFunc("GET /file/v1/projects/{project}/locations/{location}/operations/{operation}", restlab.Wrap(principalFrom, s.getOperation))
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
	if err := restlab.Require(s.Authz, p, "file.instances.create", project); err != nil {
		restlab.WriteAuthzErr(w, err)
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
			parts := strings.Split(id, "/")
			instanceID = parts[len(parts)-1]
		}
	}
	if instanceID == "" {
		gcperrors.InvalidArgument(w, "instanceId query parameter is required")
		return
	}
	description, _ := body["description"].(string)
	tier, _ := body["tier"].(string)
	if tier == "" {
		tier = "BASIC_HDD"
	}
	labelsJSON := "{}"
	if labels, ok := body["labels"]; ok {
		raw, _ := json.Marshal(labels)
		labelsJSON = string(raw)
	}
	fileSharesJSON := "[]"
	if fs, ok := body["fileShares"]; ok {
		raw, _ := json.Marshal(fs)
		fileSharesJSON = string(raw)
	}
	networksJSON := "[]"
	if nets, ok := body["networks"]; ok {
		raw, _ := json.Marshal(nets)
		networksJSON = string(raw)
	}
	name := instanceName(project, location, instanceID)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	created, err := s.Store.CreateFilestoreInstance(store.FilestoreInstance{
		Name: name, ProjectID: project, Location: location, InstanceID: instanceID,
		Description: description, Tier: tier, State: "READY",
		LabelsJSON: labelsJSON, FileSharesJSON: fileSharesJSON, NetworksJSON: networksJSON,
		CreatedAt: now,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "instance already exists")
		return
	}
	out, ok, err := s.Store.GetFilestoreInstance(name)
	if err != nil || !ok {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "created instance missing")
		return
	}
	resp := toInstanceJSON(out)
	resp["@type"] = "type.googleapis.com/google.cloud.filestore.v1.Instance"
	writeDoneOperation(w, project, location, "create-"+instanceID, resp)
}

func (s *Service) getOperation(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	opID, _ := splitAction(r.PathValue("operation"))
	if err := restlab.Require(s.Authz, p, "file.operations.get", project); err != nil {
		restlab.WriteAuthzErr(w, err)
		return
	}
	opName := fmt.Sprintf("projects/%s/locations/%s/operations/%s", project, location, opID)
	restlab.WriteJSON(w, http.StatusOK, map[string]any{
		"name": opName,
		"done": true,
	})
}

func (s *Service) getInstance(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	id, _ := splitAction(r.PathValue("instance"))
	if err := restlab.Require(s.Authz, p, "file.instances.get", project); err != nil {
		restlab.WriteAuthzErr(w, err)
		return
	}
	inst, ok, err := s.Store.GetFilestoreInstance(instanceName(project, location, id))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Instance not found")
		return
	}
	restlab.WriteJSON(w, http.StatusOK, toInstanceJSON(inst))
}

func (s *Service) listInstances(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	if err := restlab.Require(s.Authz, p, "file.instances.list", project); err != nil {
		restlab.WriteAuthzErr(w, err)
		return
	}
	list, err := s.Store.ListFilestoreInstances(project, location)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, inst := range list {
		items = append(items, toInstanceJSON(inst))
	}
	restlab.WriteJSON(w, http.StatusOK, map[string]any{"instances": items})
}

func (s *Service) deleteInstance(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	id, _ := splitAction(r.PathValue("instance"))
	if err := restlab.Require(s.Authz, p, "file.instances.delete", project); err != nil {
		restlab.WriteAuthzErr(w, err)
		return
	}
	ok, err := s.Store.DeleteFilestoreInstance(instanceName(project, location, id))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Instance not found")
		return
	}
	restlab.WriteJSON(w, http.StatusOK, map[string]any{})
}

// writeDoneOperation returns a completed LRO so Terraform Filestore wait
// does not treat the instance resource name as an unfinished operation.
func writeDoneOperation(w http.ResponseWriter, project, location, opID string, response any) {
	restlab.WriteJSON(w, http.StatusOK, map[string]any{
		"name":     fmt.Sprintf("projects/%s/locations/%s/operations/%s", project, location, opID),
		"done":     true,
		"response": response,
	})
}

func toInstanceJSON(inst store.FilestoreInstance) map[string]any {
	var labels any = map[string]string{}
	_ = json.Unmarshal([]byte(inst.LabelsJSON), &labels)
	var fileShares any = []any{}
	_ = json.Unmarshal([]byte(inst.FileSharesJSON), &fileShares)
	var networks any = []any{}
	_ = json.Unmarshal([]byte(inst.NetworksJSON), &networks)
	return map[string]any{
		"name":        inst.Name,
		"description": inst.Description,
		"state":       inst.State,
		"createTime":  inst.CreatedAt,
		"tier":        inst.Tier,
		"labels":      labels,
		"fileShares":  fileShares,
		"networks":    networks,
	}
}
