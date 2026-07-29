package cdn

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

// Service serves Cloud CDN distribution CRUD and edge GET on /cdn/.
type Service struct {
	Store *store.Store
	Authz *authz.Evaluator
	// LBInvoke optional; when nil edge LB origins use internal resolution only.
	LBInvoke func(w http.ResponseWriter, r *http.Request, project, ruleName, objectPath string)
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// Mount registers distribution REST routes and the public edge path.
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	mux.HandleFunc("GET /v1/projects/{project}/global/distributions", s.wrap(principalFrom, s.listDistributions))
	mux.HandleFunc("POST /v1/projects/{project}/global/distributions", s.wrap(principalFrom, s.createDistribution))
	mux.HandleFunc("GET /v1/projects/{project}/global/distributions/{distribution}", s.wrap(principalFrom, s.getDistribution))
	mux.HandleFunc("DELETE /v1/projects/{project}/global/distributions/{distribution}", s.wrap(principalFrom, s.deleteDistribution))

	mux.HandleFunc("GET /cdn/{id}/{path...}", s.handleEdge)
	mux.HandleFunc("HEAD /cdn/{id}/{path...}", s.handleEdge)
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

func distributionName(project, id string) string {
	return fmt.Sprintf("projects/%s/global/distributions/%s", project, id)
}

func (s *Service) createDistribution(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "compute.backendBuckets.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	distID := r.URL.Query().Get("distributionId")
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	if distID == "" {
		if name, ok := body["name"].(string); ok && name != "" {
			parts := strings.Split(name, "/")
			distID = parts[len(parts)-1]
		}
	}
	if distID == "" {
		distID = "cdn-" + store.NewLBResourceID()
	}
	desc, _ := body["description"].(string)
	originType, _ := body["originType"].(string)
	originJSON := "{}"
	if origin, ok := body["origin"]; ok {
		raw, _ := json.Marshal(origin)
		originJSON = string(raw)
		if originType == "" {
			if m, ok := origin.(map[string]any); ok {
				if _, gcs := m["gcs"]; gcs {
					originType = "gcs"
				}
				if _, lb := m["lb"]; lb {
					originType = "lb"
				}
			}
		}
	}
	if originType == "" {
		originType = "gcs"
	}
	enabled := true
	if v, ok := body["enabled"].(bool); ok {
		enabled = v
	}
	name := distributionName(project, distID)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	created, err := s.Store.CreateCDNDistribution(store.CDNDistribution{
		Name: name, ProjectID: project, DistributionID: distID, Description: desc,
		OriginType: originType, OriginJSON: originJSON, Enabled: enabled, CreatedAt: now,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "distribution already exists")
		return
	}
	d, ok, err := s.Store.GetCDNDistribution(name)
	if err != nil || !ok {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "created distribution missing")
		return
	}
	writeJSON(w, http.StatusOK, toDistributionJSON(d))
}

func (s *Service) getDistribution(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	distID := r.PathValue("distribution")
	if err := s.require(p, "compute.backendBuckets.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	d, ok, err := s.Store.GetCDNDistributionByID(project, distID)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "distribution not found")
		return
	}
	writeJSON(w, http.StatusOK, toDistributionJSON(d))
}

func (s *Service) listDistributions(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "compute.backendBuckets.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	rows, err := s.Store.ListCDNDistributions(project)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]any, 0, len(rows))
	for _, d := range rows {
		items = append(items, toDistributionJSON(d))
	}
	writeJSON(w, http.StatusOK, map[string]any{"distributions": items})
}

func (s *Service) deleteDistribution(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	distID := r.PathValue("distribution")
	if err := s.require(p, "compute.backendBuckets.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := distributionName(project, distID)
	ok, err := s.Store.DeleteCDNDistribution(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "distribution not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Service) handleEdge(w http.ResponseWriter, r *http.Request) {
	distID := r.PathValue("id")
	objectPath := strings.TrimPrefix(r.PathValue("path"), "/")
	d, ok, err := s.Store.GetCDNDistributionByEdgeID(distID)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "distribution not found")
		return
	}
	w.Header().Set("X-Noctaxris-GCP-CDN", distID)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	var origin map[string]any
	_ = json.Unmarshal([]byte(d.OriginJSON), &origin)
	switch d.OriginType {
	case "lb":
		lbOrigin, _ := origin["lb"].(map[string]any)
		project, _ := lbOrigin["project"].(string)
		rule, _ := lbOrigin["forwardingRule"].(string)
		if project == "" || rule == "" {
			gcperrors.InvalidArgument(w, "lb origin requires project and forwardingRule")
			return
		}
		if s.LBInvoke != nil {
			s.LBInvoke(w, r, project, rule, objectPath)
			return
		}
		fr, ok, err := s.Store.GetLBForwardingRuleByID(project, "global", rule)
		if err != nil || !ok {
			gcperrors.NotFound(w, "forwarding rule not found")
			return
		}
		backendName, err := lbSvcResolveBackend(s.Store, fr.Target)
		if err != nil || backendName == "" {
			gcperrors.NotFound(w, "backend not configured")
			return
		}
		bs, ok, err := s.Store.GetLBBackendService(backendName)
		if err != nil || !ok {
			gcperrors.NotFound(w, "backend service not found")
			return
		}
		serveGCSFromCDN(w, r, s.Store, bs.BackendsJSON, objectPath)
	case "gcs", "":
		gcsOrigin, _ := origin["gcs"].(map[string]any)
		bucket, _ := gcsOrigin["bucket"].(string)
		prefix, _ := gcsOrigin["objectPrefix"].(string)
		if bucket == "" {
			gcperrors.InvalidArgument(w, "gcs origin requires bucket")
			return
		}
		backendsJSON := fmt.Sprintf(`[{"gcsBucket":%q,"objectPrefix":%q}]`, bucket, prefix)
		serveGCSFromCDN(w, r, s.Store, backendsJSON, objectPath)
	default:
		gcperrors.InvalidArgument(w, "unsupported origin type")
	}
}

func lbSvcResolveBackend(st *store.Store, target string) (string, error) {
	target = strings.TrimSpace(target)
	if strings.Contains(target, "/urlMaps/") {
		m, ok, err := st.GetLBURLMap(target)
		if err != nil || !ok {
			return "", fmt.Errorf("url map not found")
		}
		return m.DefaultService, nil
	}
	if strings.Contains(target, "/backendServices/") {
		return target, nil
	}
	return "", fmt.Errorf("unsupported target")
}

func serveGCSFromCDN(w http.ResponseWriter, r *http.Request, st *store.Store, backendsJSON, objectPath string) {
	bucket, prefix, ok := store.ParseGCSOriginFromBackends(backendsJSON)
	if !ok {
		gcperrors.NotFound(w, "no GCS origin")
		return
	}
	objName := strings.TrimPrefix(objectPath, "/")
	if prefix != "" {
		objName = strings.TrimSuffix(prefix, "/") + "/" + objName
	}
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

func toDistributionJSON(d store.CDNDistribution) map[string]any {
	out := map[string]any{
		"name":       d.Name,
		"id":         d.DistributionID,
		"enabled":    d.Enabled,
		"originType": d.OriginType,
		"edgePath":   "/cdn/" + d.DistributionID + "/",
	}
	if d.Description != "" {
		out["description"] = d.Description
	}
	var origin map[string]any
	if json.Unmarshal([]byte(d.OriginJSON), &origin) == nil && len(origin) > 0 {
		out["origin"] = origin
	}
	return out
}
