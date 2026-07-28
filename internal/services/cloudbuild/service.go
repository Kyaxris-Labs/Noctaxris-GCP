package cloudbuild

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// DefaultLocation is the lab default Cloud Build location for triggers.
const DefaultLocation = "global"

// Service serves Cloud Build REST v1 (builds theatre + triggers CRUD lite).
type Service struct {
	Store *store.Store
	Authz *authz.Evaluator
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// Mount registers Cloud Build v1 REST routes.
// Colon methods are parsed from wildcard path segments via splitColonAction.
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	mux.HandleFunc("POST /v1/projects/{project}/builds", s.wrap(principalFrom, s.createBuildGlobal))
	mux.HandleFunc("GET /v1/projects/{project}/builds", s.wrap(principalFrom, s.listBuildsGlobal))
	mux.HandleFunc("GET /v1/projects/{project}/builds/{build}", s.wrap(principalFrom, s.getBuildGlobal))
	mux.HandleFunc("POST /v1/projects/{project}/builds/{build}", s.wrap(principalFrom, s.buildPOSTActionGlobal))

	mux.HandleFunc("POST /v1/projects/{project}/locations/{location}/builds", s.wrap(principalFrom, s.createBuildRegional))
	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/builds", s.wrap(principalFrom, s.listBuildsRegional))
	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/builds/{build}", s.wrap(principalFrom, s.getBuildRegional))
	mux.HandleFunc("POST /v1/projects/{project}/locations/{location}/builds/{build}", s.wrap(principalFrom, s.buildPOSTActionRegional))

	// Global triggers only: regional .../locations/{loc}/triggers collides with Eventarc on the shared mux.
	mux.HandleFunc("GET /v1/projects/{project}/triggers", s.wrap(principalFrom, s.listTriggers))
	mux.HandleFunc("POST /v1/projects/{project}/triggers", s.wrap(principalFrom, s.createTrigger))
	mux.HandleFunc("GET /v1/projects/{project}/triggers/{trigger}", s.wrap(principalFrom, s.getTrigger))
	mux.HandleFunc("POST /v1/projects/{project}/triggers/{trigger}", s.wrap(principalFrom, s.triggerPOSTAction))
	mux.HandleFunc("DELETE /v1/projects/{project}/triggers/{trigger}", s.wrap(principalFrom, s.deleteTrigger))
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

func splitColonAction(seg string) (name, action string) {
	if i := strings.IndexByte(seg, ':'); i >= 0 {
		return seg[:i], seg[i+1:]
	}
	return seg, ""
}

func buildName(project, location, id string) string {
	if location == "" || location == "global" {
		return fmt.Sprintf("projects/%s/builds/%s", project, id)
	}
	return fmt.Sprintf("projects/%s/locations/%s/builds/%s", project, location, id)
}

func triggerName(project, location, id string) string {
	if location == "" || location == "global" {
		return fmt.Sprintf("projects/%s/triggers/%s", project, id)
	}
	return fmt.Sprintf("projects/%s/locations/%s/triggers/%s", project, location, id)
}

func (s *Service) createBuildGlobal(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	s.createBuild(w, r, p, r.PathValue("project"), "global")
}

func (s *Service) createBuildRegional(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	s.createBuild(w, r, p, r.PathValue("project"), r.PathValue("location"))
}

func (s *Service) createBuild(w http.ResponseWriter, r *http.Request, p authn.Principal, project, location string) {
	if err := s.require(p, "cloudbuild.builds.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body == nil {
		body = map[string]any{}
	}
	buildID := store.NewCbBuildID()
	name := buildName(project, location, buildID)
	raw, _ := json.Marshal(body)
	created, err := s.Store.CreateCbBuild(store.CbBuild{
		Name: name, ProjectID: project, Location: location, BuildID: buildID,
		Status: "WORKING", StatusDetail: "lab theatre: build accepted", BuildJSON: string(raw),
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "build already exists")
		return
	}
	b, _, _ := s.Store.GetCbBuild(name)
	buildObj := toBuildJSON(b)
	writeJSON(w, http.StatusOK, map[string]any{
		"name": "operations/" + buildID,
		"metadata": map[string]any{
			"@type": "type.googleapis.com/google.devtools.cloudbuild.v1.BuildOperationMetadata",
			"build": buildObj,
		},
		"done": false,
	})
}

func (s *Service) listBuildsGlobal(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	s.listBuilds(w, r, p, r.PathValue("project"), "")
}

func (s *Service) listBuildsRegional(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	s.listBuilds(w, r, p, r.PathValue("project"), r.PathValue("location"))
}

func (s *Service) listBuilds(w http.ResponseWriter, r *http.Request, p authn.Principal, project, location string) {
	if err := s.require(p, "cloudbuild.builds.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	list, err := s.Store.ListCbBuilds(project, location)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, b := range list {
		items = append(items, toBuildJSON(b))
	}
	writeJSON(w, http.StatusOK, map[string]any{"builds": items})
}

func (s *Service) getBuildGlobal(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	s.getBuild(w, r, p, r.PathValue("project"), "global", r.PathValue("build"))
}

func (s *Service) getBuildRegional(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	s.getBuild(w, r, p, r.PathValue("project"), r.PathValue("location"), r.PathValue("build"))
}

func (s *Service) getBuild(w http.ResponseWriter, _ *http.Request, p authn.Principal, project, location, buildSeg string) {
	if err := s.require(p, "cloudbuild.builds.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	id, _ := splitColonAction(buildSeg)
	name := buildName(project, location, id)
	b, ok, err := s.Store.GetCbBuild(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		// Legacy clients may pass only build id against the global path for a regional build.
		b, ok, err = s.Store.GetCbBuildByID(project, id)
		if err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		}
		if !ok {
			gcperrors.NotFound(w, "Build not found")
			return
		}
		name = b.Name
	}
	adv, ok, err := s.Store.AdvanceCbBuildToSuccess(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Build not found")
		return
	}
	writeJSON(w, http.StatusOK, toBuildJSON(adv))
}

func (s *Service) buildPOSTActionGlobal(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	s.buildPOSTAction(w, r, p, r.PathValue("project"), "global", r.PathValue("build"))
}

func (s *Service) buildPOSTActionRegional(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	s.buildPOSTAction(w, r, p, r.PathValue("project"), r.PathValue("location"), r.PathValue("build"))
}

func (s *Service) buildPOSTAction(w http.ResponseWriter, r *http.Request, p authn.Principal, project, location, buildSeg string) {
	id, action := splitColonAction(buildSeg)
	switch action {
	case "cancel":
		s.cancelBuild(w, r, p, project, location, id)
	case "retry":
		s.retryBuild(w, r, p, project, location, id)
	default:
		gcperrors.NotFound(w, "unknown Cloud Build method")
	}
}

func (s *Service) cancelBuild(w http.ResponseWriter, _ *http.Request, p authn.Principal, project, location, id string) {
	if err := s.require(p, "cloudbuild.builds.update", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := buildName(project, location, id)
	b, ok, err := s.Store.GetCbBuild(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		b, ok, err = s.Store.GetCbBuildByID(project, id)
		if err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		}
		if !ok {
			gcperrors.NotFound(w, "Build not found")
			return
		}
		name = b.Name
	}
	out, ok, err := s.Store.CancelCbBuildDeepen(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Build not found")
		return
	}
	writeJSON(w, http.StatusOK, toBuildJSON(out))
}

func (s *Service) retryBuild(w http.ResponseWriter, _ *http.Request, p authn.Principal, project, location, id string) {
	if err := s.require(p, "cloudbuild.builds.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := buildName(project, location, id)
	src, ok, err := s.Store.GetCbBuild(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		src, ok, err = s.Store.GetCbBuildByID(project, id)
		if err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		}
		if !ok {
			gcperrors.NotFound(w, "Build not found")
			return
		}
	}
	retryLoc := src.Location
	if location != "" && location != "global" {
		retryLoc = location
	}
	buildID := store.NewCbBuildID()
	newName := buildName(project, retryLoc, buildID)
	created, err := s.Store.CreateCbBuild(store.CbBuild{
		Name: newName, ProjectID: project, Location: retryLoc, BuildID: buildID,
		Status: "WORKING", StatusDetail: "lab theatre: retry of " + src.BuildID, BuildJSON: src.BuildJSON,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "build already exists")
		return
	}
	b, _, _ := s.Store.GetCbBuild(newName)
	buildObj := toBuildJSON(b)
	writeJSON(w, http.StatusOK, map[string]any{
		"name": "operations/" + buildID,
		"metadata": map[string]any{
			"@type": "type.googleapis.com/google.devtools.cloudbuild.v1.BuildOperationMetadata",
			"build": buildObj,
		},
		"done": false,
	})
}

func (s *Service) createTrigger(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	if location == "" {
		location = DefaultLocation
	}
	if err := s.require(p, "cloudbuild.triggers.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body == nil {
		body = map[string]any{}
	}
	triggerID := ""
	if id, _ := body["id"].(string); id != "" {
		triggerID = id
	}
	if triggerID == "" {
		if n, _ := body["name"].(string); n != "" {
			triggerID = n
		}
	}
	if triggerID == "" {
		triggerID = store.NewCbTriggerID()
	}
	name := triggerName(project, location, triggerID)
	body["id"] = triggerID
	body["name"] = triggerID
	body["resourceName"] = name
	raw, _ := json.Marshal(body)
	created, err := s.Store.CreateCbTrigger(store.CbTrigger{
		Name: name, ProjectID: project, Location: location, TriggerID: triggerID, TriggerJSON: string(raw),
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "trigger already exists")
		return
	}
	out, _, _ := s.Store.GetCbTrigger(name)
	writeJSON(w, http.StatusOK, toTriggerJSON(out))
}

func (s *Service) listTriggers(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	if location == "" {
		location = DefaultLocation
	}
	if err := s.require(p, "cloudbuild.triggers.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	list, err := s.Store.ListCbTriggers(project, location)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, t := range list {
		items = append(items, toTriggerJSON(t))
	}
	writeJSON(w, http.StatusOK, map[string]any{"triggers": items})
}

func (s *Service) getTrigger(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	if location == "" {
		location = DefaultLocation
	}
	id, action := splitColonAction(r.PathValue("trigger"))
	if action != "" {
		gcperrors.NotFound(w, "unknown Cloud Build method")
		return
	}
	if err := s.require(p, "cloudbuild.triggers.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	t, ok, err := s.Store.GetCbTrigger(triggerName(project, location, id))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Trigger not found")
		return
	}
	writeJSON(w, http.StatusOK, toTriggerJSON(t))
}

func (s *Service) triggerPOSTAction(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	if location == "" {
		location = DefaultLocation
	}
	id, action := splitColonAction(r.PathValue("trigger"))
	switch action {
	case "run":
		s.runTrigger(w, r, p, project, location, id)
	default:
		gcperrors.NotFound(w, "unknown Cloud Build method")
	}
}

func (s *Service) runTrigger(w http.ResponseWriter, r *http.Request, p authn.Principal, project, location, id string) {
	if err := s.require(p, "cloudbuild.builds.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	trig, ok, err := s.Store.GetCbTrigger(triggerName(project, location, id))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Trigger not found")
		return
	}
	// Consume optional RepoSource body; theatre ignores SCM and creates a WORKING build.
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body == nil {
		body = map[string]any{}
	}
	buildCfg := map[string]any{
		"steps": []any{
			map[string]any{"name": "gcr.io/cloud-builders/gcloud", "args": []any{"version"}},
		},
		"tags": []any{"trigger-" + trig.TriggerID, "lab-trigger-run"},
	}
	if filename, _ := extractTriggerFilename(trig.TriggerJSON); filename != "" {
		buildCfg["filename"] = filename
	}
	if len(body) > 0 {
		buildCfg["source"] = map[string]any{"repoSource": body}
	}
	raw, _ := json.Marshal(buildCfg)
	buildID := store.NewCbBuildID()
	name := buildName(project, "global", buildID)
	created, err := s.Store.CreateCbBuild(store.CbBuild{
		Name: name, ProjectID: project, Location: "global", BuildID: buildID,
		Status: "WORKING", StatusDetail: "lab theatre: trigger run (no webhook)", BuildJSON: string(raw),
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "build already exists")
		return
	}
	b, _, _ := s.Store.GetCbBuild(name)
	buildObj := toBuildJSON(b)
	writeJSON(w, http.StatusOK, map[string]any{
		"name": "operations/" + buildID,
		"metadata": map[string]any{
			"@type": "type.googleapis.com/google.devtools.cloudbuild.v1.BuildOperationMetadata",
			"build": buildObj,
		},
		"done": false,
	})
}

func extractTriggerFilename(triggerJSON string) (string, bool) {
	var cfg map[string]any
	if err := json.Unmarshal([]byte(triggerJSON), &cfg); err != nil {
		return "", false
	}
	if f, ok := cfg["filename"].(string); ok && f != "" {
		return f, true
	}
	return "", false
}

func (s *Service) deleteTrigger(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	if location == "" {
		location = DefaultLocation
	}
	id, _ := splitColonAction(r.PathValue("trigger"))
	if err := s.require(p, "cloudbuild.triggers.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	ok, err := s.Store.DeleteCbTrigger(triggerName(project, location, id))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Trigger not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func toBuildJSON(b store.CbBuild) map[string]any {
	var cfg map[string]any
	_ = json.Unmarshal([]byte(b.BuildJSON), &cfg)
	if cfg == nil {
		cfg = map[string]any{}
	}
	out := map[string]any{}
	for k, v := range cfg {
		out[k] = v
	}
	out["id"] = b.BuildID
	out["name"] = b.Name
	out["projectId"] = b.ProjectID
	out["status"] = b.Status
	out["statusDetail"] = b.StatusDetail
	out["createTime"] = b.CreateTime
	out["startTime"] = b.StartTime
	if b.FinishTime != "" {
		out["finishTime"] = b.FinishTime
	}
	out["logUrl"] = b.LogURL
	out["projectNumber"] = b.ProjectNumber
	return out
}

func toTriggerJSON(t store.CbTrigger) map[string]any {
	var cfg map[string]any
	_ = json.Unmarshal([]byte(t.TriggerJSON), &cfg)
	if cfg == nil {
		cfg = map[string]any{}
	}
	cfg["id"] = t.TriggerID
	cfg["name"] = t.TriggerID
	cfg["resourceName"] = t.Name
	cfg["createTime"] = t.CreatedAt
	return cfg
}
