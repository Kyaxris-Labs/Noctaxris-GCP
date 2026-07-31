package loadbalancing

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

// Service serves HTTP(S) load balancing control plane + lab dataplane on /lb/.
type Service struct {
	Store *store.Store
	Authz *authz.Evaluator
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// Mount registers Compute v1 LB metadata routes and the public lab invoke path.
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	scope := "global"
	mux.HandleFunc("GET /compute/v1/projects/{project}/"+scope+"/backendServices", s.wrap(principalFrom, s.listBackendServices))
	mux.HandleFunc("POST /compute/v1/projects/{project}/"+scope+"/backendServices", s.wrap(principalFrom, s.insertBackendService))
	mux.HandleFunc("GET /compute/v1/projects/{project}/"+scope+"/backendServices/{backendService}", s.wrap(principalFrom, s.getBackendService))
	mux.HandleFunc("PATCH /compute/v1/projects/{project}/"+scope+"/backendServices/{backendService}", s.wrap(principalFrom, s.patchBackendService))
	mux.HandleFunc("POST /compute/v1/projects/{project}/"+scope+"/backendServices/{backendService}/setSecurityPolicy", s.wrap(principalFrom, s.setBackendServiceSecurityPolicy))
	mux.HandleFunc("DELETE /compute/v1/projects/{project}/"+scope+"/backendServices/{backendService}", s.wrap(principalFrom, s.deleteBackendService))

	mux.HandleFunc("GET /compute/v1/projects/{project}/"+scope+"/targetHttpsProxies", s.wrap(principalFrom, s.listTargetHTTPSProxies))
	mux.HandleFunc("POST /compute/v1/projects/{project}/"+scope+"/targetHttpsProxies", s.wrap(principalFrom, s.insertTargetHTTPSProxy))
	mux.HandleFunc("GET /compute/v1/projects/{project}/"+scope+"/targetHttpsProxies/{targetHttpsProxy}", s.wrap(principalFrom, s.getTargetHTTPSProxy))
	mux.HandleFunc("PATCH /compute/v1/projects/{project}/"+scope+"/targetHttpsProxies/{targetHttpsProxy}", s.wrap(principalFrom, s.patchTargetHTTPSProxy))
	mux.HandleFunc("DELETE /compute/v1/projects/{project}/"+scope+"/targetHttpsProxies/{targetHttpsProxy}", s.wrap(principalFrom, s.deleteTargetHTTPSProxy))

	mux.HandleFunc("GET /compute/v1/projects/{project}/"+scope+"/urlMaps", s.wrap(principalFrom, s.listURLMaps))
	mux.HandleFunc("POST /compute/v1/projects/{project}/"+scope+"/urlMaps", s.wrap(principalFrom, s.insertURLMap))
	mux.HandleFunc("GET /compute/v1/projects/{project}/"+scope+"/urlMaps/{urlMap}", s.wrap(principalFrom, s.getURLMap))
	mux.HandleFunc("DELETE /compute/v1/projects/{project}/"+scope+"/urlMaps/{urlMap}", s.wrap(principalFrom, s.deleteURLMap))

	mux.HandleFunc("GET /compute/v1/projects/{project}/"+scope+"/forwardingRules", s.wrap(principalFrom, s.listForwardingRules))
	mux.HandleFunc("POST /compute/v1/projects/{project}/"+scope+"/forwardingRules", s.wrap(principalFrom, s.insertForwardingRule))
	mux.HandleFunc("GET /compute/v1/projects/{project}/"+scope+"/forwardingRules/{forwardingRule}", s.wrap(principalFrom, s.getForwardingRule))
	mux.HandleFunc("DELETE /compute/v1/projects/{project}/"+scope+"/forwardingRules/{forwardingRule}", s.wrap(principalFrom, s.deleteForwardingRule))

	mux.HandleFunc("GET /lb/{project}/{name}/{path...}", s.handleLBInvoke)
	mux.HandleFunc("HEAD /lb/{project}/{name}/{path...}", s.handleLBInvoke)
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

func selfLink(parts ...string) string {
	return "https://www.googleapis.com/compute/v1/" + strings.Join(parts, "/")
}

func resourceNameFromSelfLink(raw string) string {
	raw = strings.TrimSpace(raw)
	const prefix = "https://www.googleapis.com/compute/v1/"
	if strings.HasPrefix(raw, prefix) {
		return strings.TrimPrefix(raw, prefix)
	}
	return raw
}

func doneOperation(project, opType, targetLink string) map[string]any {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	id := store.NewGCEResourceID()
	name := "operation-" + id
	return map[string]any{
		"kind":          "compute#operation",
		"id":            id,
		"name":          name,
		"operationType": opType,
		"targetLink":    targetLink,
		"status":        "DONE",
		"progress":      100,
		"insertTime":    now,
		"startTime":     now,
		"endTime":       now,
		"selfLink":      selfLink("projects", project, "global", "operations", name),
	}
}

func (s *Service) insertBackendService(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "compute.backendServices.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	serviceID, _ := body["name"].(string)
	if serviceID == "" {
		serviceID = "bs-" + store.NewLBResourceID()
	}
	name := fmt.Sprintf("projects/%s/global/backendServices/%s", project, serviceID)
	desc, _ := body["description"].(string)
	protocol, _ := body["protocol"].(string)
	backendsJSON := "[]"
	if b, ok := body["backends"]; ok {
		raw, _ := json.Marshal(b)
		backendsJSON = string(raw)
	}
	securityPolicy, _ := body["securityPolicy"].(string)
	securityPolicy = resourceNameFromSelfLink(securityPolicy)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	created, err := s.Store.CreateLBBackendService(store.LBBackendService{
		Name: name, ProjectID: project, Region: "global", ServiceID: serviceID,
		Description: desc, Protocol: protocol, BackendsJSON: backendsJSON, SecurityPolicy: securityPolicy, CreatedAt: now,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "backend service already exists")
		return
	}
	writeJSON(w, http.StatusOK, doneOperation(project, "insert", selfLink("projects", project, "global", "backendServices", serviceID)))
}

func (s *Service) patchBackendService(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	serviceID := r.PathValue("backendService")
	if err := s.require(p, "compute.backendServices.update", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	name := fmt.Sprintf("projects/%s/global/backendServices/%s", project, serviceID)
	bs, ok, err := s.Store.GetLBBackendService(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "backend service not found")
		return
	}
	securityPolicy := bs.SecurityPolicy
	if v, ok := body["securityPolicy"].(string); ok {
		securityPolicy = resourceNameFromSelfLink(v)
	}
	updated, err := s.Store.UpdateLBBackendServiceSecurityPolicy(name, securityPolicy)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !updated {
		gcperrors.NotFound(w, "backend service not found")
		return
	}
	bs.SecurityPolicy = securityPolicy
	writeJSON(w, http.StatusOK, toBackendServiceJSON(bs))
}

func (s *Service) setBackendServiceSecurityPolicy(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	serviceID := r.PathValue("backendService")
	if err := s.require(p, "compute.backendServices.update", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	securityPolicy, _ := body["securityPolicy"].(string)
	securityPolicy = resourceNameFromSelfLink(securityPolicy)
	name := fmt.Sprintf("projects/%s/global/backendServices/%s", project, serviceID)
	if _, ok, err := s.Store.GetLBBackendService(name); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "backend service not found")
		return
	}
	updated, err := s.Store.UpdateLBBackendServiceSecurityPolicy(name, securityPolicy)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !updated {
		gcperrors.NotFound(w, "backend service not found")
		return
	}
	writeJSON(w, http.StatusOK, doneOperation(project, "compute.backendServices.setSecurityPolicy", selfLink("projects", project, "global", "backendServices", serviceID)))
}

func (s *Service) getBackendService(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	serviceID := r.PathValue("backendService")
	if err := s.require(p, "compute.backendServices.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := fmt.Sprintf("projects/%s/global/backendServices/%s", project, serviceID)
	bs, ok, err := s.Store.GetLBBackendService(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "backend service not found")
		return
	}
	writeJSON(w, http.StatusOK, toBackendServiceJSON(bs))
}

func (s *Service) listBackendServices(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "compute.backendServices.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	rows, err := s.Store.ListLBBackendServices(project, "global")
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]any, 0, len(rows))
	for _, bs := range rows {
		items = append(items, toBackendServiceJSON(bs))
	}
	writeJSON(w, http.StatusOK, map[string]any{"kind": "compute#backendServiceList", "items": items})
}

func (s *Service) deleteBackendService(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	serviceID := r.PathValue("backendService")
	if err := s.require(p, "compute.backendServices.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := fmt.Sprintf("projects/%s/global/backendServices/%s", project, serviceID)
	ok, err := s.Store.DeleteLBBackendService(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "backend service not found")
		return
	}
	writeJSON(w, http.StatusOK, doneOperation(project, "delete", selfLink("projects", project, "global", "backendServices", serviceID)))
}

func (s *Service) insertTargetHTTPSProxy(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "compute.targetHttpsProxies.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	proxyID, _ := body["name"].(string)
	if proxyID == "" {
		proxyID = "https-proxy-" + store.NewLBResourceID()
	}
	name := fmt.Sprintf("projects/%s/global/targetHttpsProxies/%s", project, proxyID)
	desc, _ := body["description"].(string)
	urlMap, _ := body["urlMap"].(string)
	securityPolicy, _ := body["securityPolicy"].(string)
	sslJSON := "[]"
	if certs, ok := body["sslCertificates"]; ok {
		raw, _ := json.Marshal(certs)
		sslJSON = string(raw)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	created, err := s.Store.CreateLBTargetHTTPSProxy(store.LBTargetHTTPSProxy{
		Name: name, ProjectID: project, Region: "global", ProxyID: proxyID,
		Description: desc, URLMap: urlMap, SecurityPolicy: securityPolicy, SSLCertificatesJSON: sslJSON, CreatedAt: now,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "target https proxy already exists")
		return
	}
	writeJSON(w, http.StatusOK, doneOperation(project, "insert", selfLink("projects", project, "global", "targetHttpsProxies", proxyID)))
}

func (s *Service) getTargetHTTPSProxy(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	proxyID := r.PathValue("targetHttpsProxy")
	if err := s.require(p, "compute.targetHttpsProxies.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := fmt.Sprintf("projects/%s/global/targetHttpsProxies/%s", project, proxyID)
	proxy, ok, err := s.Store.GetLBTargetHTTPSProxy(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "target https proxy not found")
		return
	}
	writeJSON(w, http.StatusOK, toTargetHTTPSProxyJSON(proxy))
}

func (s *Service) listTargetHTTPSProxies(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "compute.targetHttpsProxies.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	rows, err := s.Store.ListLBTargetHTTPSProxies(project, "global")
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]any, 0, len(rows))
	for _, proxy := range rows {
		items = append(items, toTargetHTTPSProxyJSON(proxy))
	}
	writeJSON(w, http.StatusOK, map[string]any{"kind": "compute#targetHttpsProxyList", "items": items})
}

func (s *Service) patchTargetHTTPSProxy(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	proxyID := r.PathValue("targetHttpsProxy")
	if err := s.require(p, "compute.targetHttpsProxies.update", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	name := fmt.Sprintf("projects/%s/global/targetHttpsProxies/%s", project, proxyID)
	proxy, ok, err := s.Store.GetLBTargetHTTPSProxy(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "target https proxy not found")
		return
	}
	desc := proxy.Description
	urlMap := proxy.URLMap
	securityPolicy := proxy.SecurityPolicy
	if v, ok := body["description"].(string); ok {
		desc = v
	}
	if v, ok := body["urlMap"].(string); ok {
		urlMap = v
	}
	if v, ok := body["securityPolicy"].(string); ok {
		securityPolicy = v
	}
	updated, err := s.Store.UpdateLBTargetHTTPSProxy(name, desc, urlMap, securityPolicy)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !updated {
		gcperrors.NotFound(w, "target https proxy not found")
		return
	}
	proxy.Description = desc
	proxy.URLMap = urlMap
	proxy.SecurityPolicy = securityPolicy
	writeJSON(w, http.StatusOK, toTargetHTTPSProxyJSON(proxy))
}

func (s *Service) deleteTargetHTTPSProxy(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	proxyID := r.PathValue("targetHttpsProxy")
	if err := s.require(p, "compute.targetHttpsProxies.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := fmt.Sprintf("projects/%s/global/targetHttpsProxies/%s", project, proxyID)
	ok, err := s.Store.DeleteLBTargetHTTPSProxy(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "target https proxy not found")
		return
	}
	writeJSON(w, http.StatusOK, doneOperation(project, "delete", selfLink("projects", project, "global", "targetHttpsProxies", proxyID)))
}

func (s *Service) insertURLMap(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "compute.urlMaps.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	mapID, _ := body["name"].(string)
	if mapID == "" {
		mapID = "map-" + store.NewLBResourceID()
	}
	name := fmt.Sprintf("projects/%s/global/urlMaps/%s", project, mapID)
	desc, _ := body["description"].(string)
	defaultSvc, _ := body["defaultService"].(string)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	created, err := s.Store.CreateLBURLMap(store.LBURLMap{
		Name: name, ProjectID: project, Region: "global", MapID: mapID,
		Description: desc, DefaultService: defaultSvc, CreatedAt: now,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "url map already exists")
		return
	}
	m, ok, err := s.Store.GetLBURLMap(name)
	if err != nil || !ok {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "created url map missing")
		return
	}
	writeJSON(w, http.StatusOK, toURLMapJSON(m))
}

func (s *Service) getURLMap(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	mapID := r.PathValue("urlMap")
	if err := s.require(p, "compute.urlMaps.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := fmt.Sprintf("projects/%s/global/urlMaps/%s", project, mapID)
	m, ok, err := s.Store.GetLBURLMap(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "url map not found")
		return
	}
	writeJSON(w, http.StatusOK, toURLMapJSON(m))
}

func (s *Service) listURLMaps(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "compute.urlMaps.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	rows, err := s.Store.ListLBURLMaps(project, "global")
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]any, 0, len(rows))
	for _, m := range rows {
		items = append(items, toURLMapJSON(m))
	}
	writeJSON(w, http.StatusOK, map[string]any{"kind": "compute#urlMapList", "items": items})
}

func (s *Service) deleteURLMap(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	mapID := r.PathValue("urlMap")
	if err := s.require(p, "compute.urlMaps.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := fmt.Sprintf("projects/%s/global/urlMaps/%s", project, mapID)
	ok, err := s.Store.DeleteLBURLMap(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "url map not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Service) insertForwardingRule(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "compute.forwardingRules.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	ruleID, _ := body["name"].(string)
	if ruleID == "" {
		ruleID = "fr-" + store.NewLBResourceID()
	}
	name := fmt.Sprintf("projects/%s/global/forwardingRules/%s", project, ruleID)
	desc, _ := body["description"].(string)
	target, _ := body["target"].(string)
	ip, _ := body["IPAddress"].(string)
	portRange, _ := body["portRange"].(string)
	scheme, _ := body["loadBalancingScheme"].(string)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	created, err := s.Store.CreateLBForwardingRule(store.LBForwardingRule{
		Name: name, ProjectID: project, Region: "global", RuleID: ruleID,
		Description: desc, Target: target, IPAddress: ip, PortRange: portRange,
		LoadBalancingScheme: scheme, CreatedAt: now,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "forwarding rule already exists")
		return
	}
	fr, ok, err := s.Store.GetLBForwardingRule(name)
	if err != nil || !ok {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "created forwarding rule missing")
		return
	}
	writeJSON(w, http.StatusOK, toForwardingRuleJSON(fr))
}

func (s *Service) getForwardingRule(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	ruleID := r.PathValue("forwardingRule")
	if err := s.require(p, "compute.forwardingRules.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := fmt.Sprintf("projects/%s/global/forwardingRules/%s", project, ruleID)
	fr, ok, err := s.Store.GetLBForwardingRule(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "forwarding rule not found")
		return
	}
	writeJSON(w, http.StatusOK, toForwardingRuleJSON(fr))
}

func (s *Service) listForwardingRules(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "compute.forwardingRules.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	rows, err := s.Store.ListLBForwardingRules(project, "global")
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]any, 0, len(rows))
	for _, fr := range rows {
		items = append(items, toForwardingRuleJSON(fr))
	}
	writeJSON(w, http.StatusOK, map[string]any{"kind": "compute#forwardingRuleList", "items": items})
}

func (s *Service) deleteForwardingRule(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	ruleID := r.PathValue("forwardingRule")
	if err := s.require(p, "compute.forwardingRules.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := fmt.Sprintf("projects/%s/global/forwardingRules/%s", project, ruleID)
	ok, err := s.Store.DeleteLBForwardingRule(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "forwarding rule not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Service) handleLBInvoke(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	ruleName := r.PathValue("name")
	objectPath := strings.TrimPrefix(r.PathValue("path"), "/")
	fr, ok, err := s.Store.GetLBForwardingRuleByID(project, "global", ruleName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "forwarding rule not found")
		return
	}
	backendName, err := s.resolveBackendService(fr.Target)
	if err != nil || backendName == "" {
		gcperrors.NotFound(w, "url map or backend service not configured")
		return
	}
	bs, ok, err := s.Store.GetLBBackendService(backendName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "backend service not found")
		return
	}
	w.Header().Set("X-Noctaxris-GCP-LB", ruleName)
	serveGCSBackend(w, r, s.Store, bs.BackendsJSON, objectPath)
}

func (s *Service) resolveBackendService(target string) (string, error) {
	target = resourceNameFromSelfLink(target)
	if target == "" {
		return "", fmt.Errorf("empty target")
	}
	if strings.Contains(target, "/targetHttpsProxies/") {
		proxy, ok, err := s.Store.GetLBTargetHTTPSProxy(target)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf("target https proxy not found")
		}
		return s.resolveBackendService(proxy.URLMap)
	}
	if strings.Contains(target, "/urlMaps/") {
		m, ok, err := s.Store.GetLBURLMap(target)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf("url map not found")
		}
		return m.DefaultService, nil
	}
	if strings.Contains(target, "/backendServices/") {
		return target, nil
	}
	return "", fmt.Errorf("unsupported target")
}

func toBackendServiceJSON(bs store.LBBackendService) map[string]any {
	out := map[string]any{
		"kind":     "compute#backendService",
		"name":     bs.ServiceID,
		"selfLink": selfLink("projects", bs.ProjectID, "global", "backendServices", bs.ServiceID),
		"protocol": bs.Protocol,
	}
	if bs.Description != "" {
		out["description"] = bs.Description
	}
	var backends []any
	_ = json.Unmarshal([]byte(bs.BackendsJSON), &backends)
	if len(backends) > 0 {
		out["backends"] = backends
	}
	if bs.SecurityPolicy != "" {
		pol := resourceNameFromSelfLink(bs.SecurityPolicy)
		out["securityPolicy"] = "https://www.googleapis.com/compute/v1/" + strings.TrimPrefix(pol, "/")
	}
	return out
}

func toTargetHTTPSProxyJSON(p store.LBTargetHTTPSProxy) map[string]any {
	out := map[string]any{
		"kind":     "compute#targetHttpsProxy",
		"name":     p.ProxyID,
		"selfLink": p.Name,
	}
	if p.Description != "" {
		out["description"] = p.Description
	}
	if p.URLMap != "" {
		out["urlMap"] = p.URLMap
	}
	if p.SecurityPolicy != "" {
		out["securityPolicy"] = p.SecurityPolicy
	}
	var sslCerts []any
	_ = json.Unmarshal([]byte(p.SSLCertificatesJSON), &sslCerts)
	if len(sslCerts) > 0 {
		out["sslCertificates"] = sslCerts
	}
	return out
}

func toURLMapJSON(m store.LBURLMap) map[string]any {
	out := map[string]any{
		"kind":     "compute#urlMap",
		"name":     m.MapID,
		"selfLink": m.Name,
	}
	if m.Description != "" {
		out["description"] = m.Description
	}
	if m.DefaultService != "" {
		out["defaultService"] = m.DefaultService
	}
	return out
}

func toForwardingRuleJSON(fr store.LBForwardingRule) map[string]any {
	out := map[string]any{
		"kind":                "compute#forwardingRule",
		"name":                fr.RuleID,
		"selfLink":            fr.Name,
		"IPAddress":           fr.IPAddress,
		"target":              fr.Target,
		"portRange":           fr.PortRange,
		"loadBalancingScheme": fr.LoadBalancingScheme,
	}
	if fr.Description != "" {
		out["description"] = fr.Description
	}
	return out
}

// serveGCSBackend streams a lab GCS object when backends declare gcsBucket.
func serveGCSBackend(w http.ResponseWriter, r *http.Request, st *store.Store, backendsJSON, objectPath string) {
	bucket, prefix, ok := store.ParseGCSOriginFromBackends(backendsJSON)
	if !ok {
		gcperrors.NotFound(w, "no GCS backend configured")
		return
	}
	objName := strings.TrimPrefix(objectPath, "/")
	if prefix != "" {
		objName = strings.TrimSuffix(prefix, "/") + "/" + objName
	}
	objName = strings.TrimPrefix(objName, "/")
	meta, found, err := st.GetObject(bucket, objName, 0)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !found {
		gcperrors.NotFound(w, "object not found")
		return
	}
	data, err := st.ReadObjectBytes(meta)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if meta.ContentType != "" {
		w.Header().Set("Content-Type", meta.ContentType)
	}
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
