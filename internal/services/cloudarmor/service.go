package cloudarmor

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// Service serves Cloud Armor securityPolicies under Compute Engine REST v1.
type Service struct {
	Store *store.Store
	Authz *authz.Evaluator
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// Mount registers global securityPolicies REST routes.
// Colon methods (:validate) are parsed from the securityPolicy path segment
// because ServeMux patterns cannot embed ':'.
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	mux.HandleFunc("GET /compute/v1/projects/{project}/global/securityPolicies", s.wrap(principalFrom, s.listPolicies))
	mux.HandleFunc("POST /compute/v1/projects/{project}/global/securityPolicies", s.wrap(principalFrom, s.insertPolicy))
	mux.HandleFunc("GET /compute/v1/projects/{project}/global/securityPolicies/{securityPolicy}", s.wrap(principalFrom, s.getPolicy))
	mux.HandleFunc("DELETE /compute/v1/projects/{project}/global/securityPolicies/{securityPolicy}", s.wrap(principalFrom, s.deletePolicy))
	mux.HandleFunc("POST /compute/v1/projects/{project}/global/securityPolicies/{securityPolicy}", s.wrap(principalFrom, s.policyPOSTColon))
	mux.HandleFunc("POST /compute/v1/projects/{project}/global/securityPolicies/{securityPolicy}/addRule", s.wrap(principalFrom, s.addRule))
	mux.HandleFunc("POST /compute/v1/projects/{project}/global/securityPolicies/{securityPolicy}/removeRule", s.wrap(principalFrom, s.removeRule))
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

func splitColonAction(seg string) (name, action string) {
	if i := strings.IndexByte(seg, ':'); i >= 0 {
		return seg[:i], seg[i+1:]
	}
	return seg, ""
}

func selfLink(parts ...string) string {
	return "https://www.googleapis.com/compute/v1/" + strings.Join(parts, "/")
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

func (s *Service) insertPolicy(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "compute.securityPolicies.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	body, err := decodeBody(r)
	if err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	id, _ := body["name"].(string)
	id = strings.TrimSpace(id)
	if id == "" {
		gcperrors.InvalidArgument(w, "name is required")
		return
	}
	desc, _ := body["description"].(string)
	policyType, _ := body["type"].(string)
	if policyType == "" {
		policyType = "CLOUD_ARMOR"
	}
	rulesJSON := store.DefaultCloudArmorRulesJSON()
	if rawRules, ok := body["rules"]; ok {
		b, err := json.Marshal(rawRules)
		if err != nil {
			gcperrors.InvalidArgument(w, "invalid rules")
			return
		}
		rulesJSON = string(b)
		if !hasDefaultRule(rulesJSON) {
			rulesJSON = appendDefaultRule(rulesJSON)
		}
	}
	extras := map[string]any{}
	for k, v := range body {
		switch k {
		case "name", "description", "type", "rules", "kind", "id", "selfLink", "creationTimestamp":
			continue
		default:
			extras[k] = v
		}
	}
	extrasJSON, _ := json.Marshal(extras)
	name := store.CloudArmorPolicyResourceName(project, id)
	created, err := s.Store.CreateCloudArmorSecurityPolicy(store.CloudArmorSecurityPolicy{
		Name: name, ProjectID: project, PolicyID: id, Description: desc,
		PolicyType: policyType, RulesJSON: rulesJSON, BodyJSON: string(extrasJSON),
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "security policy already exists")
		return
	}
	writeJSON(w, http.StatusOK, doneOperation(project, "insert", selfLink("projects", project, "global", "securityPolicies", id)))
}

func (s *Service) listPolicies(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "compute.securityPolicies.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	list, err := s.Store.ListCloudArmorSecurityPolicies(project)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, pol := range list {
		items = append(items, toPolicyJSON(pol))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"kind":     "compute#securityPolicyList",
		"id":       "projects/" + project + "/global/securityPolicies",
		"items":    items,
		"selfLink": selfLink("projects", project, "global", "securityPolicies"),
	})
}

func (s *Service) getPolicy(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	id, action := splitColonAction(r.PathValue("securityPolicy"))
	if action != "" {
		gcperrors.NotFound(w, "unknown Cloud Armor method")
		return
	}
	if err := s.require(p, "compute.securityPolicies.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	pol, ok, err := s.Store.GetCloudArmorSecurityPolicyByProjectID(project, id)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "SecurityPolicy not found")
		return
	}
	writeJSON(w, http.StatusOK, toPolicyJSON(pol))
}

func (s *Service) deletePolicy(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	id, _ := splitColonAction(r.PathValue("securityPolicy"))
	if err := s.require(p, "compute.securityPolicies.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	target := selfLink("projects", project, "global", "securityPolicies", id)
	ok, err := s.Store.DeleteCloudArmorSecurityPolicy(store.CloudArmorPolicyResourceName(project, id))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "SecurityPolicy not found")
		return
	}
	writeJSON(w, http.StatusOK, doneOperation(project, "delete", target))
}

func (s *Service) policyPOSTColon(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	id, action := splitColonAction(r.PathValue("securityPolicy"))
	switch action {
	case "validate":
		s.validatePolicy(w, r, p, project, id)
	default:
		gcperrors.NotFound(w, "unknown Cloud Armor method")
	}
}

func (s *Service) addRule(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	id, _ := splitColonAction(r.PathValue("securityPolicy"))
	if err := s.require(p, "compute.securityPolicies.update", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := store.CloudArmorPolicyResourceName(project, id)
	pol, ok, err := s.Store.GetCloudArmorSecurityPolicy(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "SecurityPolicy not found")
		return
	}
	body, err := decodeBody(r)
	if err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	prio := rulePriority(body["priority"])
	if prio < 0 {
		gcperrors.InvalidArgument(w, "priority is required")
		return
	}
	rules := unmarshalRules(pol.RulesJSON)
	for _, existing := range rules {
		if rulePriority(existing["priority"]) == prio {
			gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "rule priority already exists")
			return
		}
	}
	body["kind"] = "compute#securityPolicyRule"
	if _, ok := body["preview"]; !ok {
		body["preview"] = false
	}
	rules = append(rules, body)
	raw, _ := json.Marshal(rules)
	_, ok, err = s.Store.UpdateCloudArmorSecurityPolicyRules(name, string(raw), pol.Description)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "SecurityPolicy not found")
		return
	}
	writeJSON(w, http.StatusOK, doneOperation(project, "addRule", selfLink("projects", project, "global", "securityPolicies", id)))
}

func (s *Service) removeRule(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	id, _ := splitColonAction(r.PathValue("securityPolicy"))
	if err := s.require(p, "compute.securityPolicies.update", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	prioStr := r.URL.Query().Get("priority")
	prio, err := strconv.Atoi(prioStr)
	if err != nil {
		gcperrors.InvalidArgument(w, "priority query parameter is required")
		return
	}
	if prio == 2147483647 {
		gcperrors.InvalidArgument(w, "cannot remove default rule")
		return
	}
	name := store.CloudArmorPolicyResourceName(project, id)
	pol, ok, err := s.Store.GetCloudArmorSecurityPolicy(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "SecurityPolicy not found")
		return
	}
	rules := unmarshalRules(pol.RulesJSON)
	filtered := make([]map[string]any, 0, len(rules))
	found := false
	for _, rule := range rules {
		if rulePriority(rule["priority"]) == prio {
			found = true
			continue
		}
		filtered = append(filtered, rule)
	}
	if !found {
		gcperrors.NotFound(w, "SecurityPolicyRule not found")
		return
	}
	raw, _ := json.Marshal(filtered)
	_, ok, err = s.Store.UpdateCloudArmorSecurityPolicyRules(name, string(raw), pol.Description)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "SecurityPolicy not found")
		return
	}
	writeJSON(w, http.StatusOK, doneOperation(project, "removeRule", selfLink("projects", project, "global", "securityPolicies", id)))
}

func (s *Service) validatePolicy(w http.ResponseWriter, r *http.Request, p authn.Principal, project, id string) {
	if err := s.require(p, "compute.securityPolicies.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	pol, ok, err := s.Store.GetCloudArmorSecurityPolicyByProjectID(project, id)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "SecurityPolicy not found")
		return
	}
	body, err := decodeBody(r)
	if err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	writeJSON(w, http.StatusOK, evalSecurityPolicy(unmarshalRules(pol.RulesJSON), body))
}

func toPolicyJSON(p store.CloudArmorSecurityPolicy) map[string]any {
	out := map[string]any{}
	_ = json.Unmarshal([]byte(p.BodyJSON), &out)
	if out == nil {
		out = map[string]any{}
	}
	out["kind"] = "compute#securityPolicy"
	out["id"] = p.NumericID
	out["name"] = p.PolicyID
	out["description"] = p.Description
	out["type"] = p.PolicyType
	out["rules"] = unmarshalRules(p.RulesJSON)
	out["creationTimestamp"] = p.CreatedAt
	out["selfLink"] = selfLink("projects", p.ProjectID, "global", "securityPolicies", p.PolicyID)
	return out
}

func unmarshalRules(raw string) []map[string]any {
	var rules []map[string]any
	_ = json.Unmarshal([]byte(raw), &rules)
	if rules == nil {
		return []map[string]any{}
	}
	return rules
}

func hasDefaultRule(rulesJSON string) bool {
	for _, rule := range unmarshalRules(rulesJSON) {
		if rulePriority(rule["priority"]) == 2147483647 {
			return true
		}
	}
	return false
}

func appendDefaultRule(rulesJSON string) string {
	rules := unmarshalRules(rulesJSON)
	def := unmarshalRules(store.DefaultCloudArmorRulesJSON())
	rules = append(rules, def...)
	raw, _ := json.Marshal(rules)
	return string(raw)
}

func rulePriority(v any) int {
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
	case string:
		i, err := strconv.Atoi(n)
		if err != nil {
			return -1
		}
		return i
	default:
		return -1
	}
}

// SampleRequest fields for :validate theatre.
// uriPath / headers / srcIp drive ByteMatchSet and SRC_IPS_V1 matching.
func evalSecurityPolicy(rules []map[string]any, sample map[string]any) map[string]any {
	sorted := make([]map[string]any, len(rules))
	copy(sorted, rules)
	sort.SliceStable(sorted, func(i, j int) bool {
		return rulePriority(sorted[i]["priority"]) < rulePriority(sorted[j]["priority"])
	})

	for _, rule := range sorted {
		matched, reason := ruleMatches(rule, sample)
		if !matched {
			continue
		}
		preview, _ := rule["preview"].(bool)
		action, _ := rule["action"].(string)
		if action == "" {
			action = "allow"
		}
		if preview {
			continue
		}
		allowed := strings.HasPrefix(strings.ToLower(action), "allow")
		return map[string]any{
			"matched":  true,
			"allowed":  allowed,
			"action":   action,
			"priority": rulePriority(rule["priority"]),
			"preview":  false,
			"reason":   reason,
		}
	}
	return map[string]any{
		"matched": false,
		"allowed": false,
		"action":  "deny",
		"reason":  "no matching rule (fail closed)",
	}
}

func ruleMatches(rule, sample map[string]any) (bool, string) {
	match, _ := rule["match"].(map[string]any)
	if match == nil {
		return false, "rule has no match"
	}
	if bms, ok := match["byteMatchSet"].(map[string]any); ok {
		okMatch, reason := byteMatchSetMatches(bms, sample)
		return okMatch, reason
	}
	versioned, _ := match["versionedExpr"].(string)
	if versioned == "SRC_IPS_V1" {
		cfg, _ := match["config"].(map[string]any)
		ranges := extractStringSlice(cfg["srcIpRanges"])
		srcIP, _ := sample["srcIp"].(string)
		if srcIP == "" {
			srcIP, _ = sample["sourceIp"].(string)
		}
		for _, cidr := range ranges {
			if cidr == "*" || cidr == srcIP {
				return true, "matched SRC_IPS_V1"
			}
		}
		return false, "srcIp not in srcIpRanges"
	}
	if expr, ok := match["expr"].(map[string]any); ok {
		expression, _ := expr["expression"].(string)
		if strings.TrimSpace(expression) != "" {
			// Lab theatre: non-empty CEL expressions do not evaluate; treat as non-match.
			return false, "CEL expr not evaluated in lab"
		}
	}
	return false, "unsupported match"
}

func byteMatchSetMatches(bms, sample map[string]any) (bool, string) {
	field, _ := bms["fieldToMatch"].(string)
	constraint, _ := bms["positionalConstraint"].(string)
	search, _ := bms["searchString"].(string)
	constraint = strings.ToUpper(strings.TrimSpace(constraint))
	if constraint == "" {
		constraint = "CONTAINS"
	}
	var haystack string
	switch strings.ToUpper(field) {
	case "URIPATH", "URI_PATH":
		haystack, _ = sample["uriPath"].(string)
		if haystack == "" {
			haystack, _ = sample["uri"].(string)
		}
	case "SINGLEHEADER", "SINGLE_HEADER":
		headerName, _ := bms["headerName"].(string)
		headers, _ := sample["headers"].(map[string]any)
		if headers != nil && headerName != "" {
			for k, v := range headers {
				if strings.EqualFold(k, headerName) {
					haystack = fmt.Sprint(v)
					break
				}
			}
		}
	default:
		return false, "unsupported byteMatchSet fieldToMatch"
	}
	switch constraint {
	case "EXACTLY":
		if haystack == search {
			return true, "byteMatchSet EXACTLY"
		}
	case "CONTAINS":
		if strings.Contains(haystack, search) {
			return true, "byteMatchSet CONTAINS"
		}
	default:
		return false, "unsupported positionalConstraint"
	}
	return false, "byteMatchSet no match"
}

func extractStringSlice(v any) []string {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return t
	default:
		return nil
	}
}
