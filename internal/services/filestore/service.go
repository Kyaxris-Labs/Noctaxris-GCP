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
	mux.HandleFunc("GET /file/v1/projects/{project}/locations/{location}/instances", s.wrap(principalFrom, s.listInstances))
	mux.HandleFunc("POST /file/v1/projects/{project}/locations/{location}/instances", s.wrap(principalFrom, s.createInstance))
	mux.HandleFunc("GET /file/v1/projects/{project}/locations/{location}/instances/{instance}", s.wrap(principalFrom, s.getInstance))
	mux.HandleFunc("DELETE /file/v1/projects/{project}/locations/{location}/instances/{instance}", s.wrap(principalFrom, s.deleteInstance))
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
	if err := s.require(p, "file.instances.create", project); err != nil {
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
	writeJSON(w, http.StatusOK, toInstanceJSON(out))
}

func (s *Service) getInstance(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	id, _ := splitAction(r.PathValue("instance"))
	if err := s.require(p, "file.instances.get", project); err != nil {
		writeAuthzErr(w, err)
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
	writeJSON(w, http.StatusOK, toInstanceJSON(inst))
}

func (s *Service) listInstances(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	if err := s.require(p, "file.instances.list", project); err != nil {
		writeAuthzErr(w, err)
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
	writeJSON(w, http.StatusOK, map[string]any{"instances": items})
}

func (s *Service) deleteInstance(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	id, _ := splitAction(r.PathValue("instance"))
	if err := s.require(p, "file.instances.delete", project); err != nil {
		writeAuthzErr(w, err)
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
	writeJSON(w, http.StatusOK, map[string]any{})
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
