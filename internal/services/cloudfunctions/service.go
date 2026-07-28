package cloudfunctions

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

// DefaultLocation is the lab default Functions location.
const DefaultLocation = "us-central1"

// Service serves Cloud Functions v2 REST (control plane + HTTP invoke stub).
type Service struct {
	Store *store.Store
	Authz *authz.Evaluator
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// Mount registers Cloud Functions v2 REST routes.
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	mux.HandleFunc("GET /v2/projects/{project}/locations/{location}/functions", s.wrap(principalFrom, s.listFunctions))
	mux.HandleFunc("POST /v2/projects/{project}/locations/{location}/functions", s.wrap(principalFrom, s.createFunction))
	mux.HandleFunc("GET /v2/projects/{project}/locations/{location}/functions/{function}", s.wrap(principalFrom, s.getOrInvoke))
	mux.HandleFunc("PATCH /v2/projects/{project}/locations/{location}/functions/{function}", s.wrap(principalFrom, s.patchFunction))
	mux.HandleFunc("DELETE /v2/projects/{project}/locations/{location}/functions/{function}", s.wrap(principalFrom, s.deleteFunction))
	mux.HandleFunc("POST /v2/projects/{project}/locations/{location}/functions/{function}", s.wrap(principalFrom, s.getOrInvoke))
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

func functionName(project, location, id string) string {
	return fmt.Sprintf("projects/%s/locations/%s/functions/%s", project, location, id)
}

func splitAction(seg string) (name, action string) {
	if i := strings.IndexByte(seg, ':'); i >= 0 {
		return seg[:i], seg[i+1:]
	}
	return seg, ""
}

func (s *Service) createFunction(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	if err := s.require(p, "cloudfunctions.functions.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	functionID := r.URL.Query().Get("functionId")
	if functionID == "" {
		gcperrors.InvalidArgument(w, "functionId is required")
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body == nil {
		body = map[string]any{}
	}
	labResp := `{"ok":true}`
	if v, ok := body["labResponse"].(map[string]any); ok {
		raw, _ := json.Marshal(v)
		labResp = string(raw)
	} else if v, ok := body["labResponse"].(string); ok && v != "" {
		labResp = v
	}
	cfgRaw, _ := json.Marshal(body)
	name := functionName(project, location, functionID)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	created, err := s.Store.CreateCloudFunction(store.CloudFunction{
		Name:            name,
		ProjectID:       project,
		Location:        location,
		FunctionID:      functionID,
		State:           "ACTIVE",
		ConfigJSON:      string(cfgRaw),
		LabResponseJSON: labResp,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "function already exists")
		return
	}
	fn, ok, err := s.Store.GetCloudFunction(name)
	if err != nil || !ok {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "created function missing")
		return
	}
	writeJSON(w, http.StatusOK, toFunctionJSON(fn))
}

func (s *Service) listFunctions(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	if err := s.require(p, "cloudfunctions.functions.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	list, err := s.Store.ListCloudFunctions(project, location)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, fn := range list {
		items = append(items, toFunctionJSON(fn))
	}
	writeJSON(w, http.StatusOK, map[string]any{"functions": items})
}

func (s *Service) getOrInvoke(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	seg := r.PathValue("function")
	id, action := splitAction(seg)
	if action == "invoke" {
		s.invoke(w, r, p, project, location, id)
		return
	}
	if r.Method != http.MethodGet {
		gcperrors.NotFound(w, "unknown Cloud Functions method")
		return
	}
	if err := s.require(p, "cloudfunctions.functions.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := functionName(project, location, id)
	fn, ok, err := s.Store.GetCloudFunction(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Function not found")
		return
	}
	writeJSON(w, http.StatusOK, toFunctionJSON(fn))
}

func (s *Service) patchFunction(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	id, _ := splitAction(r.PathValue("function"))
	if err := s.require(p, "cloudfunctions.functions.update", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	cfgRaw := ""
	labResp := ""
	if body != nil {
		b, _ := json.Marshal(body)
		cfgRaw = string(b)
		if v, ok := body["labResponse"].(map[string]any); ok {
			raw, _ := json.Marshal(v)
			labResp = string(raw)
		} else if v, ok := body["labResponse"].(string); ok {
			labResp = v
		}
	}
	name := functionName(project, location, id)
	fn, ok, err := s.Store.UpdateCloudFunction(name, cfgRaw, labResp)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Function not found")
		return
	}
	writeJSON(w, http.StatusOK, toFunctionJSON(fn))
}

func (s *Service) deleteFunction(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	id, _ := splitAction(r.PathValue("function"))
	if err := s.require(p, "cloudfunctions.functions.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := functionName(project, location, id)
	ok, err := s.Store.DeleteCloudFunction(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Function not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Service) invoke(w http.ResponseWriter, _ *http.Request, p authn.Principal, project, location, id string) {
	if err := s.require(p, "cloudfunctions.functions.invoke", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := functionName(project, location, id)
	fn, ok, err := s.Store.GetCloudFunction(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Function not found")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if fn.LabResponseJSON != "" {
		_, _ = w.Write([]byte(fn.LabResponseJSON))
		return
	}
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func toFunctionJSON(fn store.CloudFunction) map[string]any {
	var cfg any
	_ = json.Unmarshal([]byte(fn.ConfigJSON), &cfg)
	out := map[string]any{
		"name":       fn.Name,
		"state":      fn.State,
		"createTime": fn.CreatedAt,
		"updateTime": fn.UpdatedAt,
		"url":        fn.URI,
		"environment": "GEN_2",
	}
	if m, ok := cfg.(map[string]any); ok {
		if bc, ok := m["buildConfig"]; ok {
			out["buildConfig"] = bc
		}
		if sc, ok := m["serviceConfig"]; ok {
			out["serviceConfig"] = sc
		}
	}
	return out
}
