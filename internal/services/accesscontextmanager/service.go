package accesscontextmanager

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
	"github.com/google/uuid"
)

// Service serves Access Context Manager REST v1 (accessPolicies + servicePerimeters)
// for VPC Service Controls perimeter lite theatre.
type Service struct {
	Store *store.Store
	Authz *authz.Evaluator
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// Mount registers Access Context Manager v1 REST routes.
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	if s.Store != nil {
		_ = s.Store.MigrateAccessContextManager()
	}
	mux.HandleFunc("GET /v1/accessPolicies", s.wrap(principalFrom, s.listPolicies))
	mux.HandleFunc("POST /v1/accessPolicies", s.wrap(principalFrom, s.createPolicy))
	mux.HandleFunc("GET /v1/accessPolicies/{policy}", s.wrap(principalFrom, s.getPolicy))
	mux.HandleFunc("PATCH /v1/accessPolicies/{policy}", s.wrap(principalFrom, s.patchPolicy))
	mux.HandleFunc("DELETE /v1/accessPolicies/{policy}", s.wrap(principalFrom, s.deletePolicy))

	mux.HandleFunc("GET /v1/accessPolicies/{policy}/servicePerimeters", s.wrap(principalFrom, s.listPerimeters))
	mux.HandleFunc("POST /v1/accessPolicies/{policy}/servicePerimeters", s.wrap(principalFrom, s.createPerimeter))
	mux.HandleFunc("GET /v1/accessPolicies/{policy}/servicePerimeters/{perimeter}", s.wrap(principalFrom, s.getPerimeter))
	mux.HandleFunc("PATCH /v1/accessPolicies/{policy}/servicePerimeters/{perimeter}", s.wrap(principalFrom, s.patchPerimeter))
	mux.HandleFunc("DELETE /v1/accessPolicies/{policy}/servicePerimeters/{perimeter}", s.wrap(principalFrom, s.deletePerimeter))
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

func authzResource(parent string) string {
	parent = strings.TrimSpace(parent)
	if parent == "" {
		return "organizations/noctaxris-gcp-org"
	}
	return parent
}

func writeDoneOperation(w http.ResponseWriter, name string, response map[string]any) {
	writeJSON(w, http.StatusOK, map[string]any{
		"name":     name,
		"done":     true,
		"response": response,
	})
}

func (s *Service) createPolicy(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	body, err := decodeBody(r)
	if err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	parent, _ := body["parent"].(string)
	parent = strings.TrimSpace(parent)
	if parent == "" {
		parent = r.URL.Query().Get("parent")
	}
	if parent == "" {
		parent = "organizations/noctaxris-gcp-org"
	}
	if err := s.require(p, "accesscontextmanager.policies.create", authzResource(parent)); err != nil {
		writeAuthzErr(w, err)
		return
	}
	title, _ := body["title"].(string)
	policyID := strings.TrimSpace(r.URL.Query().Get("policyId"))
	if policyID == "" {
		if n, _ := body["name"].(string); n != "" {
			policyID = strings.TrimPrefix(n, "accessPolicies/")
		}
	}
	if policyID == "" {
		policyID = strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	}
	scopesJSON := "[]"
	if scopes, ok := body["scopes"]; ok {
		raw, err := json.Marshal(scopes)
		if err != nil {
			gcperrors.InvalidArgument(w, "invalid scopes")
			return
		}
		scopesJSON = string(raw)
	}
	extras := map[string]any{}
	for k, v := range body {
		switch k {
		case "name", "parent", "title", "scopes", "etag", "createTime", "updateTime":
			continue
		default:
			extras[k] = v
		}
	}
	extrasJSON, _ := json.Marshal(extras)
	name := store.AccessPolicyResourceName(policyID)
	created, err := s.Store.CreateAccessPolicy(store.AccessPolicy{
		Name: name, PolicyID: policyID, Parent: parent, Title: title,
		ScopesJSON: scopesJSON, BodyJSON: string(extrasJSON),
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "access policy already exists")
		return
	}
	out, ok, err := s.Store.GetAccessPolicy(name)
	if err != nil || !ok {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "created access policy missing")
		return
	}
	writeDoneOperation(w, name+"/operations/create", toPolicyJSON(out))
}

func (s *Service) listPolicies(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	parent := r.URL.Query().Get("parent")
	if err := s.require(p, "accesscontextmanager.policies.list", authzResource(parent)); err != nil {
		writeAuthzErr(w, err)
		return
	}
	list, err := s.Store.ListAccessPolicies(parent)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for i := range list {
		items = append(items, toPolicyJSON(list[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"accessPolicies": items})
}

func (s *Service) getPolicy(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	policyID := r.PathValue("policy")
	name := store.AccessPolicyResourceName(policyID)
	pol, ok, err := s.Store.GetAccessPolicy(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "access policy not found")
		return
	}
	if err := s.require(p, "accesscontextmanager.policies.get", authzResource(pol.Parent)); err != nil {
		writeAuthzErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toPolicyJSON(pol))
}

func (s *Service) patchPolicy(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	policyID := r.PathValue("policy")
	name := store.AccessPolicyResourceName(policyID)
	pol, ok, err := s.Store.GetAccessPolicy(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "access policy not found")
		return
	}
	if err := s.require(p, "accesscontextmanager.policies.update", authzResource(pol.Parent)); err != nil {
		writeAuthzErr(w, err)
		return
	}
	body, err := decodeBody(r)
	if err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	mask := r.URL.Query().Get("updateMask")
	updateTitle := mask == "" || fieldInMask(mask, "title")
	updateScopes := mask == "" || fieldInMask(mask, "scopes")
	title, _ := body["title"].(string)
	scopesJSON := pol.ScopesJSON
	if updateScopes {
		if scopes, ok := body["scopes"]; ok {
			raw, err := json.Marshal(scopes)
			if err != nil {
				gcperrors.InvalidArgument(w, "invalid scopes")
				return
			}
			scopesJSON = string(raw)
		}
	}
	updated, ok, err := s.Store.UpdateAccessPolicy(name, title, scopesJSON, updateTitle, updateScopes)
	if err != nil || !ok {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "update failed")
		return
	}
	writeDoneOperation(w, name+"/operations/patch", toPolicyJSON(updated))
}

func (s *Service) deletePolicy(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	policyID := r.PathValue("policy")
	name := store.AccessPolicyResourceName(policyID)
	pol, ok, err := s.Store.GetAccessPolicy(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "access policy not found")
		return
	}
	if err := s.require(p, "accesscontextmanager.policies.delete", authzResource(pol.Parent)); err != nil {
		writeAuthzErr(w, err)
		return
	}
	if _, err := s.Store.DeleteAccessPolicy(name); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeDoneOperation(w, name+"/operations/delete", map[string]any{"@type": "type.googleapis.com/google.protobuf.Empty"})
}

func (s *Service) createPerimeter(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	policyID := r.PathValue("policy")
	policyName := store.AccessPolicyResourceName(policyID)
	pol, ok, err := s.Store.GetAccessPolicy(policyName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "access policy not found")
		return
	}
	if err := s.require(p, "accesscontextmanager.servicePerimeters.create", authzResource(pol.Parent)); err != nil {
		writeAuthzErr(w, err)
		return
	}
	body, err := decodeBody(r)
	if err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	perimeterID := strings.TrimSpace(r.URL.Query().Get("servicePerimeterId"))
	if perimeterID == "" {
		if n, _ := body["name"].(string); n != "" {
			parts := strings.Split(n, "/")
			perimeterID = parts[len(parts)-1]
		}
	}
	if perimeterID == "" {
		gcperrors.InvalidArgument(w, "servicePerimeterId is required")
		return
	}
	title, _ := body["title"].(string)
	desc, _ := body["description"].(string)
	ptype, _ := body["perimeterType"].(string)
	if ptype == "" {
		ptype = "PERIMETER_TYPE_REGULAR"
	}
	statusJSON := marshalOptional(body["status"])
	specJSON := marshalOptional(body["spec"])
	useDry, _ := body["useExplicitDryRunSpec"].(bool)
	extras := map[string]any{}
	for k, v := range body {
		switch k {
		case "name", "title", "description", "perimeterType", "status", "spec", "useExplicitDryRunSpec", "etag":
			continue
		default:
			extras[k] = v
		}
	}
	extrasJSON, _ := json.Marshal(extras)
	name := store.ServicePerimeterResourceName(policyID, perimeterID)
	created, err := s.Store.CreateServicePerimeter(store.ServicePerimeter{
		Name: name, PolicyName: policyName, PerimeterID: perimeterID,
		Title: title, Description: desc, PerimeterType: ptype,
		StatusJSON: statusJSON, SpecJSON: specJSON, UseExplicitDryRunSpec: useDry,
		BodyJSON: string(extrasJSON),
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "service perimeter already exists")
		return
	}
	out, ok, err := s.Store.GetServicePerimeter(name)
	if err != nil || !ok {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "created perimeter missing")
		return
	}
	writeDoneOperation(w, name+"/operations/create", toPerimeterJSON(out))
}

func (s *Service) listPerimeters(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	policyID := r.PathValue("policy")
	policyName := store.AccessPolicyResourceName(policyID)
	pol, ok, err := s.Store.GetAccessPolicy(policyName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "access policy not found")
		return
	}
	if err := s.require(p, "accesscontextmanager.servicePerimeters.list", authzResource(pol.Parent)); err != nil {
		writeAuthzErr(w, err)
		return
	}
	list, err := s.Store.ListServicePerimeters(policyName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for i := range list {
		items = append(items, toPerimeterJSON(list[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"servicePerimeters": items})
}

func (s *Service) getPerimeter(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	policyID := r.PathValue("policy")
	perimeterID := r.PathValue("perimeter")
	policyName := store.AccessPolicyResourceName(policyID)
	pol, ok, err := s.Store.GetAccessPolicy(policyName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "access policy not found")
		return
	}
	if err := s.require(p, "accesscontextmanager.servicePerimeters.get", authzResource(pol.Parent)); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := store.ServicePerimeterResourceName(policyID, perimeterID)
	sp, ok, err := s.Store.GetServicePerimeter(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "service perimeter not found")
		return
	}
	writeJSON(w, http.StatusOK, toPerimeterJSON(sp))
}

func (s *Service) patchPerimeter(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	policyID := r.PathValue("policy")
	perimeterID := r.PathValue("perimeter")
	policyName := store.AccessPolicyResourceName(policyID)
	pol, ok, err := s.Store.GetAccessPolicy(policyName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "access policy not found")
		return
	}
	if err := s.require(p, "accesscontextmanager.servicePerimeters.update", authzResource(pol.Parent)); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := store.ServicePerimeterResourceName(policyID, perimeterID)
	cur, ok, err := s.Store.GetServicePerimeter(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "service perimeter not found")
		return
	}
	body, err := decodeBody(r)
	if err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	mask := r.URL.Query().Get("updateMask")
	if mask == "" || fieldInMask(mask, "title") {
		if v, ok := body["title"].(string); ok {
			cur.Title = v
		}
	}
	if mask == "" || fieldInMask(mask, "description") {
		if v, ok := body["description"].(string); ok {
			cur.Description = v
		}
	}
	if mask == "" || fieldInMask(mask, "perimeterType") {
		if v, ok := body["perimeterType"].(string); ok && v != "" {
			cur.PerimeterType = v
		}
	}
	if mask == "" || fieldInMask(mask, "status") {
		if _, ok := body["status"]; ok {
			cur.StatusJSON = marshalOptional(body["status"])
		}
	}
	if mask == "" || fieldInMask(mask, "spec") {
		if _, ok := body["spec"]; ok {
			cur.SpecJSON = marshalOptional(body["spec"])
		}
	}
	if mask == "" || fieldInMask(mask, "useExplicitDryRunSpec") {
		if v, ok := body["useExplicitDryRunSpec"].(bool); ok {
			cur.UseExplicitDryRunSpec = v
		}
	}
	updated, ok, err := s.Store.UpdateServicePerimeter(cur)
	if err != nil || !ok {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "update failed")
		return
	}
	writeDoneOperation(w, name+"/operations/patch", toPerimeterJSON(updated))
}

func (s *Service) deletePerimeter(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	policyID := r.PathValue("policy")
	perimeterID := r.PathValue("perimeter")
	policyName := store.AccessPolicyResourceName(policyID)
	pol, ok, err := s.Store.GetAccessPolicy(policyName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "access policy not found")
		return
	}
	if err := s.require(p, "accesscontextmanager.servicePerimeters.delete", authzResource(pol.Parent)); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := store.ServicePerimeterResourceName(policyID, perimeterID)
	deleted, err := s.Store.DeleteServicePerimeter(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !deleted {
		gcperrors.NotFound(w, "service perimeter not found")
		return
	}
	writeDoneOperation(w, name+"/operations/delete", map[string]any{"@type": "type.googleapis.com/google.protobuf.Empty"})
}

func toPolicyJSON(p store.AccessPolicy) map[string]any {
	var scopes any = []any{}
	_ = json.Unmarshal([]byte(p.ScopesJSON), &scopes)
	out := map[string]any{
		"name":       p.Name,
		"parent":     p.Parent,
		"title":      p.Title,
		"scopes":     scopes,
		"etag":       p.Etag,
		"createTime": p.CreatedAt,
		"updateTime": p.UpdatedAt,
	}
	var extras map[string]any
	if err := json.Unmarshal([]byte(p.BodyJSON), &extras); err == nil {
		for k, v := range extras {
			out[k] = v
		}
	}
	return out
}

func toPerimeterJSON(sp store.ServicePerimeter) map[string]any {
	out := map[string]any{
		"name":                  sp.Name,
		"title":                 sp.Title,
		"description":           sp.Description,
		"perimeterType":         sp.PerimeterType,
		"useExplicitDryRunSpec": sp.UseExplicitDryRunSpec,
		"etag":                  sp.Etag,
		"createTime":            sp.CreatedAt,
		"updateTime":            sp.UpdatedAt,
	}
	if sp.StatusJSON != "" && sp.StatusJSON != "{}" {
		var status any
		if json.Unmarshal([]byte(sp.StatusJSON), &status) == nil {
			out["status"] = status
		}
	}
	if sp.SpecJSON != "" && sp.SpecJSON != "{}" {
		var spec any
		if json.Unmarshal([]byte(sp.SpecJSON), &spec) == nil {
			out["spec"] = spec
		}
	}
	var extras map[string]any
	if err := json.Unmarshal([]byte(sp.BodyJSON), &extras); err == nil {
		for k, v := range extras {
			out[k] = v
		}
	}
	return out
}

func marshalOptional(v any) string {
	if v == nil {
		return ""
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(raw)
}

func fieldInMask(mask, field string) bool {
	for _, part := range strings.Split(mask, ",") {
		if strings.TrimSpace(part) == field {
			return true
		}
	}
	return false
}
