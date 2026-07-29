package securitycenter

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// EnvSCCInject enables lab InjectFindings (default off).
const EnvSCCInject = "NOCTAXRIS_GCP_SCC_INJECT"

// Service serves Security Command Center REST v1 (sources + findings lite).
type Service struct {
	Store         *store.Store
	Authz         *authz.Evaluator
	InjectEnabled bool
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// InjectEnabledFromEnv reports whether NOCTAXRIS_GCP_SCC_INJECT is truthy.
func InjectEnabledFromEnv() bool {
	v := strings.TrimSpace(os.Getenv(EnvSCCInject))
	return strings.EqualFold(v, "1") || strings.EqualFold(v, "true")
}

// Mount registers SCC v1 REST routes for organizations and projects parents,
// plus lab inject at /_noctaxris-gcp/lab/securitycenter:injectFindings.
// Colon methods (:setState) are parsed from the finding path segment via splitColonAction.
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	// Organization-scoped
	mux.HandleFunc("GET /v1/organizations/{org}/sources", s.wrap(principalFrom, s.listOrgSources))
	mux.HandleFunc("POST /v1/organizations/{org}/sources", s.wrap(principalFrom, s.createOrgSource))
	mux.HandleFunc("GET /v1/organizations/{org}/sources/{source}", s.wrap(principalFrom, s.getOrgSource))
	mux.HandleFunc("DELETE /v1/organizations/{org}/sources/{source}", s.wrap(principalFrom, s.deleteOrgSource))
	mux.HandleFunc("GET /v1/organizations/{org}/sources/{source}/findings", s.wrap(principalFrom, s.listOrgFindings))
	mux.HandleFunc("POST /v1/organizations/{org}/sources/{source}/findings", s.wrap(principalFrom, s.createOrgFinding))
	mux.HandleFunc("GET /v1/organizations/{org}/sources/{source}/findings/{finding}", s.wrap(principalFrom, s.getOrgFinding))
	mux.HandleFunc("POST /v1/organizations/{org}/sources/{source}/findings/{finding}", s.wrap(principalFrom, s.postOrgFinding))
	mux.HandleFunc("DELETE /v1/organizations/{org}/sources/{source}/findings/{finding}", s.wrap(principalFrom, s.deleteOrgFinding))

	// Project-scoped
	mux.HandleFunc("GET /v1/projects/{project}/sources", s.wrap(principalFrom, s.listProjectSources))
	mux.HandleFunc("POST /v1/projects/{project}/sources", s.wrap(principalFrom, s.createProjectSource))
	mux.HandleFunc("GET /v1/projects/{project}/sources/{source}", s.wrap(principalFrom, s.getProjectSource))
	mux.HandleFunc("DELETE /v1/projects/{project}/sources/{source}", s.wrap(principalFrom, s.deleteProjectSource))
	mux.HandleFunc("GET /v1/projects/{project}/sources/{source}/findings", s.wrap(principalFrom, s.listProjectFindings))
	mux.HandleFunc("POST /v1/projects/{project}/sources/{source}/findings", s.wrap(principalFrom, s.createProjectFinding))
	mux.HandleFunc("GET /v1/projects/{project}/sources/{source}/findings/{finding}", s.wrap(principalFrom, s.getProjectFinding))
	mux.HandleFunc("POST /v1/projects/{project}/sources/{source}/findings/{finding}", s.wrap(principalFrom, s.postProjectFinding))
	mux.HandleFunc("DELETE /v1/projects/{project}/sources/{source}/findings/{finding}", s.wrap(principalFrom, s.deleteProjectFinding))

	// Literal lab path (colon is not inside a wildcard segment).
	mux.HandleFunc("POST /_noctaxris-gcp/lab/securitycenter:injectFindings", s.wrap(principalFrom, s.injectFindings))
}

func splitColonAction(seg string) (name, action string) {
	if i := strings.IndexByte(seg, ':'); i >= 0 {
		return seg[:i], seg[i+1:]
	}
	return seg, ""
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

var errDenied = fmt.Errorf("permission denied")

func (s *Service) require(p authn.Principal, permission, resource string) error {
	ok, err := s.Authz.Evaluate(p.Email, p.IsRoot, permission, resource)
	if err != nil {
		return err
	}
	if !ok {
		return errDenied
	}
	return nil
}

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

func decodeBody(r *http.Request) (map[string]any, error) {
	defer r.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return map[string]any{}, nil
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	if body == nil {
		body = map[string]any{}
	}
	return body, nil
}

func orgParent(org string) string   { return "organizations/" + org }
func projectParent(project string) string { return "projects/" + project }

func (s *Service) listOrgSources(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	s.listSources(w, r, p, orgParent(r.PathValue("org")))
}
func (s *Service) createOrgSource(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	s.createSource(w, r, p, orgParent(r.PathValue("org")))
}
func (s *Service) getOrgSource(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	s.getSource(w, r, p, orgParent(r.PathValue("org")), r.PathValue("source"))
}
func (s *Service) deleteOrgSource(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	s.deleteSource(w, r, p, orgParent(r.PathValue("org")), r.PathValue("source"))
}
func (s *Service) listOrgFindings(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	s.listFindings(w, r, p, orgParent(r.PathValue("org")), r.PathValue("source"))
}
func (s *Service) createOrgFinding(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	s.createFinding(w, r, p, orgParent(r.PathValue("org")), r.PathValue("source"))
}
func (s *Service) getOrgFinding(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	fid, _ := splitColonAction(r.PathValue("finding"))
	s.getFinding(w, r, p, orgParent(r.PathValue("org")), r.PathValue("source"), fid)
}
func (s *Service) postOrgFinding(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	fid, action := splitColonAction(r.PathValue("finding"))
	if action == "setState" {
		s.setFindingState(w, r, p, orgParent(r.PathValue("org")), r.PathValue("source"), fid)
		return
	}
	gcperrors.WriteREST(w, http.StatusNotFound, gcperrors.StatusNotFound, "unknown finding method")
}
func (s *Service) deleteOrgFinding(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	fid, _ := splitColonAction(r.PathValue("finding"))
	s.deleteFinding(w, r, p, orgParent(r.PathValue("org")), r.PathValue("source"), fid)
}

func (s *Service) listProjectSources(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	s.listSources(w, r, p, projectParent(r.PathValue("project")))
}
func (s *Service) createProjectSource(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	s.createSource(w, r, p, projectParent(r.PathValue("project")))
}
func (s *Service) getProjectSource(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	s.getSource(w, r, p, projectParent(r.PathValue("project")), r.PathValue("source"))
}
func (s *Service) deleteProjectSource(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	s.deleteSource(w, r, p, projectParent(r.PathValue("project")), r.PathValue("source"))
}
func (s *Service) listProjectFindings(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	s.listFindings(w, r, p, projectParent(r.PathValue("project")), r.PathValue("source"))
}
func (s *Service) createProjectFinding(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	s.createFinding(w, r, p, projectParent(r.PathValue("project")), r.PathValue("source"))
}
func (s *Service) getProjectFinding(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	fid, _ := splitColonAction(r.PathValue("finding"))
	s.getFinding(w, r, p, projectParent(r.PathValue("project")), r.PathValue("source"), fid)
}
func (s *Service) postProjectFinding(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	fid, action := splitColonAction(r.PathValue("finding"))
	if action == "setState" {
		s.setFindingState(w, r, p, projectParent(r.PathValue("project")), r.PathValue("source"), fid)
		return
	}
	gcperrors.WriteREST(w, http.StatusNotFound, gcperrors.StatusNotFound, "unknown finding method")
}
func (s *Service) deleteProjectFinding(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	fid, _ := splitColonAction(r.PathValue("finding"))
	s.deleteFinding(w, r, p, projectParent(r.PathValue("project")), r.PathValue("source"), fid)
}

func (s *Service) listSources(w http.ResponseWriter, _ *http.Request, p authn.Principal, parent string) {
	if err := s.require(p, "securitycenter.sources.list", parent); err != nil {
		writeAuthzErr(w, err)
		return
	}
	list, err := s.Store.ListSCCSources(parent)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	sources := make([]map[string]any, 0, len(list))
	for _, src := range list {
		sources = append(sources, toSourceJSON(src))
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": sources})
}

func (s *Service) createSource(w http.ResponseWriter, r *http.Request, p authn.Principal, parent string) {
	if err := s.require(p, "securitycenter.sources.create", parent); err != nil {
		writeAuthzErr(w, err)
		return
	}
	body, err := decodeBody(r)
	if err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	sourceID := strings.TrimSpace(r.URL.Query().Get("sourceId"))
	if sourceID == "" {
		if n, _ := body["name"].(string); n != "" {
			parts := strings.Split(n, "/")
			sourceID = parts[len(parts)-1]
		}
	}
	if sourceID == "" {
		gcperrors.InvalidArgument(w, "sourceId is required")
		return
	}
	displayName, _ := body["displayName"].(string)
	description, _ := body["description"].(string)
	name := store.SCCSourceResourceName(parent, sourceID)
	created, err := s.Store.CreateSCCSource(store.SCCSource{
		Name: name, Parent: parent, SourceID: sourceID,
		DisplayName: displayName, Description: description,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "source already exists")
		return
	}
	out, ok, err := s.Store.GetSCCSource(name)
	if err != nil || !ok {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "created source missing")
		return
	}
	writeJSON(w, http.StatusOK, toSourceJSON(out))
}

func (s *Service) getSource(w http.ResponseWriter, _ *http.Request, p authn.Principal, parent, sourceID string) {
	if err := s.require(p, "securitycenter.sources.get", parent); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := store.SCCSourceResourceName(parent, sourceID)
	out, ok, err := s.Store.GetSCCSource(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.WriteREST(w, http.StatusNotFound, gcperrors.StatusNotFound, "source not found")
		return
	}
	writeJSON(w, http.StatusOK, toSourceJSON(out))
}

func (s *Service) deleteSource(w http.ResponseWriter, _ *http.Request, p authn.Principal, parent, sourceID string) {
	if err := s.require(p, "securitycenter.sources.delete", parent); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := store.SCCSourceResourceName(parent, sourceID)
	ok, err := s.Store.DeleteSCCSource(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.WriteREST(w, http.StatusNotFound, gcperrors.StatusNotFound, "source not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Service) listFindings(w http.ResponseWriter, _ *http.Request, p authn.Principal, parent, sourceID string) {
	if err := s.require(p, "securitycenter.findings.list", parent); err != nil {
		writeAuthzErr(w, err)
		return
	}
	sourceName := store.SCCSourceResourceName(parent, sourceID)
	if _, ok, err := s.Store.GetSCCSource(sourceName); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.WriteREST(w, http.StatusNotFound, gcperrors.StatusNotFound, "source not found")
		return
	}
	list, err := s.Store.ListSCCFindings(sourceName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	findings := make([]map[string]any, 0, len(list))
	for _, f := range list {
		findings = append(findings, toFindingJSON(f))
	}
	writeJSON(w, http.StatusOK, map[string]any{"listFindingsResults": wrapFindings(findings)})
}

func wrapFindings(findings []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(findings))
	for _, f := range findings {
		out = append(out, map[string]any{"finding": f})
	}
	return out
}

func (s *Service) createFinding(w http.ResponseWriter, r *http.Request, p authn.Principal, parent, sourceID string) {
	if err := s.require(p, "securitycenter.findings.create", parent); err != nil {
		writeAuthzErr(w, err)
		return
	}
	body, err := decodeBody(r)
	if err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	findingID := strings.TrimSpace(r.URL.Query().Get("findingId"))
	if findingID == "" {
		if n, _ := body["name"].(string); n != "" {
			parts := strings.Split(n, "/")
			findingID = parts[len(parts)-1]
		}
	}
	if findingID == "" {
		gcperrors.InvalidArgument(w, "findingId is required")
		return
	}
	sourceName := store.SCCSourceResourceName(parent, sourceID)
	if _, ok, err := s.Store.GetSCCSource(sourceName); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.WriteREST(w, http.StatusNotFound, gcperrors.StatusNotFound, "source not found")
		return
	}
	f, err := findingFromBody(body, parent, sourceName, findingID)
	if err != nil {
		gcperrors.InvalidArgument(w, err.Error())
		return
	}
	created, err := s.Store.CreateSCCFinding(f)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "finding already exists")
		return
	}
	out, ok, err := s.Store.GetSCCFinding(f.Name)
	if err != nil || !ok {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "created finding missing")
		return
	}
	writeJSON(w, http.StatusOK, toFindingJSON(out))
}

func (s *Service) getFinding(w http.ResponseWriter, _ *http.Request, p authn.Principal, parent, sourceID, findingID string) {
	if err := s.require(p, "securitycenter.findings.get", parent); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := store.SCCFindingResourceName(store.SCCSourceResourceName(parent, sourceID), findingID)
	out, ok, err := s.Store.GetSCCFinding(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.WriteREST(w, http.StatusNotFound, gcperrors.StatusNotFound, "finding not found")
		return
	}
	writeJSON(w, http.StatusOK, toFindingJSON(out))
}

func (s *Service) deleteFinding(w http.ResponseWriter, _ *http.Request, p authn.Principal, parent, sourceID, findingID string) {
	if err := s.require(p, "securitycenter.findings.delete", parent); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := store.SCCFindingResourceName(store.SCCSourceResourceName(parent, sourceID), findingID)
	ok, err := s.Store.DeleteSCCFinding(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.WriteREST(w, http.StatusNotFound, gcperrors.StatusNotFound, "finding not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Service) setFindingState(w http.ResponseWriter, r *http.Request, p authn.Principal, parent, sourceID, findingID string) {
	if err := s.require(p, "securitycenter.findings.setState", parent); err != nil {
		writeAuthzErr(w, err)
		return
	}
	body, err := decodeBody(r)
	if err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	state, _ := body["state"].(string)
	state = strings.TrimSpace(state)
	if state != "ACTIVE" && state != "INACTIVE" {
		gcperrors.InvalidArgument(w, "state must be ACTIVE or INACTIVE")
		return
	}
	name := store.SCCFindingResourceName(store.SCCSourceResourceName(parent, sourceID), findingID)
	out, ok, err := s.Store.UpdateSCCFindingState(name, state)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.WriteREST(w, http.StatusNotFound, gcperrors.StatusNotFound, "finding not found")
		return
	}
	writeJSON(w, http.StatusOK, toFindingJSON(out))
}

type injectReq struct {
	Parent   string           `json:"parent"`
	SourceID string           `json:"sourceId"`
	Findings []map[string]any `json:"findings"`
}

func (s *Service) injectFindings(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	if !s.InjectEnabled {
		gcperrors.PermissionDenied(w, "securitycenter InjectFindings is disabled; set NOCTAXRIS_GCP_SCC_INJECT=1")
		return
	}
	defer r.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		gcperrors.InvalidArgument(w, "invalid body")
		return
	}
	var req injectReq
	if len(strings.TrimSpace(string(raw))) > 0 {
		if err := json.Unmarshal(raw, &req); err != nil {
			gcperrors.InvalidArgument(w, "invalid JSON body")
			return
		}
	}
	parent := strings.TrimSpace(req.Parent)
	if parent == "" {
		gcperrors.InvalidArgument(w, "parent is required (organizations/{org} or projects/{project})")
		return
	}
	if err := s.require(p, "securitycenter.findings.create", parent); err != nil {
		writeAuthzErr(w, err)
		return
	}
	sourceID := strings.TrimSpace(req.SourceID)
	if sourceID == "" {
		sourceID = "lab-inject"
	}
	sourceName := store.SCCSourceResourceName(parent, sourceID)
	if _, ok, err := s.Store.GetSCCSource(sourceName); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		if _, err := s.Store.CreateSCCSource(store.SCCSource{
			Name: sourceName, Parent: parent, SourceID: sourceID,
			DisplayName: "Lab inject source", Description: "Created by InjectFindings",
		}); err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		}
	}
	if len(req.Findings) == 0 {
		gcperrors.InvalidArgument(w, "findings array is required")
		return
	}
	names := make([]string, 0, len(req.Findings))
	for i, item := range req.Findings {
		findingID, _ := item["findingId"].(string)
		findingID = strings.TrimSpace(findingID)
		if findingID == "" {
			if n, _ := item["name"].(string); n != "" {
				parts := strings.Split(n, "/")
				findingID = parts[len(parts)-1]
			}
		}
		if findingID == "" {
			findingID = fmt.Sprintf("injected-%d", i+1)
		}
		f, err := findingFromBody(item, parent, sourceName, findingID)
		if err != nil {
			gcperrors.InvalidArgument(w, err.Error())
			return
		}
		created, err := s.Store.CreateSCCFinding(f)
		if err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		}
		if !created {
			// Upsert theatre: replace state/fields via delete+create for inject idempotency lite.
			_, _ = s.Store.DeleteSCCFinding(f.Name)
			if _, err := s.Store.CreateSCCFinding(f); err != nil {
				gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
				return
			}
		}
		names = append(names, f.Name)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"findingNames": names,
		"source":       sourceName,
	})
}

func findingFromBody(body map[string]any, parent, sourceName, findingID string) (store.SCCFinding, error) {
	category, _ := body["category"].(string)
	if strings.TrimSpace(category) == "" {
		return store.SCCFinding{}, fmt.Errorf("category is required")
	}
	state, _ := body["state"].(string)
	if state == "" {
		state = "ACTIVE"
	}
	if state != "ACTIVE" && state != "INACTIVE" {
		return store.SCCFinding{}, fmt.Errorf("state must be ACTIVE or INACTIVE")
	}
	severity, _ := body["severity"].(string)
	if severity == "" {
		severity = "SEVERITY_UNSPECIFIED"
	}
	resourceName, _ := body["resourceName"].(string)
	externalURI, _ := body["externalUri"].(string)
	description, _ := body["description"].(string)
	eventTime, _ := body["eventTime"].(string)
	propsJSON := "{}"
	if props, ok := body["sourceProperties"]; ok {
		raw, err := json.Marshal(props)
		if err != nil {
			return store.SCCFinding{}, fmt.Errorf("invalid sourceProperties")
		}
		propsJSON = string(raw)
	}
	name := store.SCCFindingResourceName(sourceName, findingID)
	return store.SCCFinding{
		Name: name, Parent: parent, SourceName: sourceName, FindingID: findingID,
		ResourceName: resourceName, State: state, Category: category, Severity: severity,
		ExternalURI: externalURI, Description: description, SourcePropertiesJSON: propsJSON,
		EventTime: eventTime,
	}, nil
}

func toSourceJSON(src store.SCCSource) map[string]any {
	return map[string]any{
		"name":         src.Name,
		"displayName":  src.DisplayName,
		"description":  src.Description,
		"canonicalName": src.Name,
	}
}

func toFindingJSON(f store.SCCFinding) map[string]any {
	var props any = map[string]any{}
	_ = json.Unmarshal([]byte(f.SourcePropertiesJSON), &props)
	out := map[string]any{
		"name":             f.Name,
		"parent":           f.SourceName,
		"resourceName":     f.ResourceName,
		"state":            f.State,
		"category":         f.Category,
		"severity":         f.Severity,
		"externalUri":      f.ExternalURI,
		"description":      f.Description,
		"sourceProperties": props,
		"eventTime":        f.EventTime,
		"createTime":       f.CreateTime,
		"canonicalName":    f.Name,
	}
	return out
}
