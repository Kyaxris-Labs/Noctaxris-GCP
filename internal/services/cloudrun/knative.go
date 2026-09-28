package cloudrun

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

const knativeAPIVersion = "serving.knative.dev/v1"
const knativeServiceLabel = "serving.knative.dev/service"

// mountKnative registers Knative Serving v1 REST over the same RunService store.
func (s *Service) mountKnative(mux *http.ServeMux, principalFrom principalFunc) {
	base := "/apis/serving.knative.dev/v1/namespaces/{ns}"
	mux.HandleFunc("GET "+base+"/services", s.wrap(principalFrom, s.knativeListServices))
	mux.HandleFunc("POST "+base+"/services", s.wrap(principalFrom, s.knativeCreateService))
	mux.HandleFunc("GET "+base+"/services/{name}", s.wrap(principalFrom, s.knativeGetService))
	mux.HandleFunc("PUT "+base+"/services/{name}", s.wrap(principalFrom, s.knativeReplaceService))
	mux.HandleFunc("DELETE "+base+"/services/{name}", s.wrap(principalFrom, s.knativeDeleteService))

	mux.HandleFunc("GET "+base+"/revisions", s.wrap(principalFrom, s.knativeListRevisions))
	mux.HandleFunc("GET "+base+"/revisions/{name}", s.wrap(principalFrom, s.knativeGetRevision))
	mux.HandleFunc("DELETE "+base+"/revisions/{name}", s.wrap(principalFrom, s.knativeDeleteRevision))

	mux.HandleFunc("GET "+base+"/configurations", s.wrap(principalFrom, s.knativeListConfigurations))
	mux.HandleFunc("GET "+base+"/configurations/{name}", s.wrap(principalFrom, s.knativeGetConfiguration))
	mux.HandleFunc("GET "+base+"/routes", s.wrap(principalFrom, s.knativeListRoutes))
	mux.HandleFunc("GET "+base+"/routes/{name}", s.wrap(principalFrom, s.knativeGetRoute))
}

func regionFromHost(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return DefaultLocation
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	const runSuffix = "-run.googleapis.com"
	if strings.HasSuffix(host, runSuffix) {
		return strings.TrimSuffix(host, runSuffix)
	}
	if i := strings.Index(host, "-127."); i > 0 {
		return host[:i]
	}
	if strings.HasSuffix(host, "-localhost") {
		return strings.TrimSuffix(host, "-localhost")
	}
	return DefaultLocation
}

func (s *Service) knativeListServices(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("ns")
	location := regionFromHost(r.Host)
	if err := s.require(p, "run.services.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	list, err := s.Store.ListRunServices(project, location)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, svc := range list {
		items = append(items, toKnativeService(svc))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"apiVersion": knativeAPIVersion,
		"kind":       "ServiceList",
		"metadata":   map[string]any{},
		"items":      items,
	})
}

func (s *Service) knativeGetService(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("ns")
	location := regionFromHost(r.Host)
	id := r.PathValue("name")
	if err := s.require(p, "run.services.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	svc, ok, err := s.Store.GetRunService(serviceName(project, location, id))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Service not found")
		return
	}
	writeJSON(w, http.StatusOK, toKnativeService(svc))
}

func (s *Service) knativeCreateService(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("ns")
	location := regionFromHost(r.Host)
	if err := s.require(p, "run.services.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	body, err := decodeKnativeServiceBody(r)
	if err != nil {
		gcperrors.InvalidArgument(w, err.Error())
		return
	}
	serviceID := knativeMetadataName(body)
	if serviceID == "" {
		gcperrors.InvalidArgument(w, "metadata.name is required")
		return
	}
	template := knativeBodyToV2Template(body)
	tplRaw, _ := json.Marshal(template)
	if !s.admitTemplate(w, project, string(tplRaw)) {
		return
	}
	labBody := labResponseFromTemplate(template)
	trafficJSON := knativeBodyToTrafficJSON(body, "")
	now := time.Now().UTC().Format(time.RFC3339Nano)
	name := serviceName(project, location, serviceID)
	created, err := s.Store.CreateRunService(store.RunService{
		Name:            name,
		ProjectID:       project,
		Location:        location,
		ServiceID:       serviceID,
		TemplateJSON:    string(tplRaw),
		LabResponseBody: labBody,
		TrafficJSON:     trafficJSON,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "service already exists")
		return
	}
	out, ok, err := s.Store.GetRunService(name)
	if err != nil || !ok {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "created service missing")
		return
	}
	writeJSON(w, http.StatusCreated, toKnativeService(out))
}

func (s *Service) knativeReplaceService(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("ns")
	location := regionFromHost(r.Host)
	id := r.PathValue("name")
	if err := s.require(p, "run.services.update", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	body, err := decodeKnativeServiceBody(r)
	if err != nil {
		gcperrors.InvalidArgument(w, err.Error())
		return
	}
	if name := knativeMetadataName(body); name != "" && name != id {
		gcperrors.InvalidArgument(w, "metadata.name must match path")
		return
	}
	template := knativeBodyToV2Template(body)
	tplRaw, _ := json.Marshal(template)
	if !s.admitTemplate(w, project, string(tplRaw)) {
		return
	}
	labBody := labResponseFromTemplate(template)
	trafficJSON := knativeBodyToTrafficJSON(body, "")
	name := serviceName(project, location, id)
	svc, ok, err := s.Store.UpdateRunService(name, string(tplRaw), labBody, trafficJSON)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Service not found")
		return
	}
	writeJSON(w, http.StatusOK, toKnativeService(svc))
}

func (s *Service) knativeDeleteService(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("ns")
	location := regionFromHost(r.Host)
	id := r.PathValue("name")
	if err := s.require(p, "run.services.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	ok, err := s.Store.DeleteRunService(serviceName(project, location, id))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Service not found")
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("{}"))
}

func (s *Service) knativeListRevisions(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("ns")
	location := regionFromHost(r.Host)
	if err := s.require(p, "run.revisions.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	serviceID := parseKnativeServiceLabelSelector(r.URL.Query().Get("labelSelector"))
	revs, err := s.Store.ListRunRevisionsByProject(project, location, serviceID)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(revs))
	for _, rev := range revs {
		svcID := serviceIDFromServiceName(rev.ServiceName)
		items = append(items, toKnativeRevision(rev, project, svcID))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"apiVersion": knativeAPIVersion,
		"kind":       "RevisionList",
		"metadata":   map[string]any{},
		"items":      items,
	})
}

func (s *Service) knativeGetRevision(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("ns")
	location := regionFromHost(r.Host)
	short := r.PathValue("name")
	if err := s.require(p, "run.revisions.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	rev, ok, err := s.Store.GetRunRevisionByShortName(project, location, short)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Revision not found")
		return
	}
	writeJSON(w, http.StatusOK, toKnativeRevision(rev, project, serviceIDFromServiceName(rev.ServiceName)))
}

func (s *Service) knativeDeleteRevision(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("ns")
	location := regionFromHost(r.Host)
	short := r.PathValue("name")
	if err := s.require(p, "run.revisions.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	rev, ok, err := s.Store.GetRunRevisionByShortName(project, location, short)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Revision not found")
		return
	}
	deleted, err := s.Store.DeleteRunRevision(rev.Name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !deleted {
		gcperrors.NotFound(w, "Revision not found")
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("{}"))
}

func (s *Service) knativeListConfigurations(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("ns")
	location := regionFromHost(r.Host)
	if err := s.require(p, "run.services.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	list, err := s.Store.ListRunServices(project, location)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, svc := range list {
		items = append(items, toKnativeConfiguration(svc))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"apiVersion": knativeAPIVersion,
		"kind":       "ConfigurationList",
		"metadata":   map[string]any{},
		"items":      items,
	})
}

func (s *Service) knativeGetConfiguration(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("ns")
	location := regionFromHost(r.Host)
	id := r.PathValue("name")
	if err := s.require(p, "run.services.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	svc, ok, err := s.Store.GetRunService(serviceName(project, location, id))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Configuration not found")
		return
	}
	writeJSON(w, http.StatusOK, toKnativeConfiguration(svc))
}

func (s *Service) knativeListRoutes(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("ns")
	location := regionFromHost(r.Host)
	if err := s.require(p, "run.services.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	list, err := s.Store.ListRunServices(project, location)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, svc := range list {
		items = append(items, toKnativeRoute(svc))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"apiVersion": knativeAPIVersion,
		"kind":       "RouteList",
		"metadata":   map[string]any{},
		"items":      items,
	})
}

func (s *Service) knativeGetRoute(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("ns")
	location := regionFromHost(r.Host)
	id := r.PathValue("name")
	if err := s.require(p, "run.services.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	svc, ok, err := s.Store.GetRunService(serviceName(project, location, id))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Route not found")
		return
	}
	writeJSON(w, http.StatusOK, toKnativeRoute(svc))
}

func toKnativeService(svc store.RunService) map[string]any {
	tplSpec := knativePodSpecFromTemplateJSON(svc.TemplateJSON)
	shortLatest := shortRevisionName(svc.LatestRevision)
	trafficSpec, trafficStatus := knativeTrafficFromJSON(svc.TrafficJSON, shortLatest)
	return map[string]any{
		"apiVersion": knativeAPIVersion,
		"kind":       "Service",
		"metadata": map[string]any{
			"name":              svc.ServiceID,
			"namespace":         svc.ProjectID,
			"uid":               svc.UID,
			"generation":        svc.Generation,
			"creationTimestamp": svc.CreatedAt,
			"labels": map[string]any{
				"cloud.googleapis.com/location": svc.Location,
			},
			"annotations": map[string]any{
				"serving.knative.dev/creator":      "lab",
				"serving.knative.dev/lastModifier": "lab",
			},
		},
		"spec": map[string]any{
			"template": map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]any{},
				},
				"spec": tplSpec,
			},
			"traffic": trafficSpec,
		},
		"status": map[string]any{
			"observedGeneration":        svc.Generation,
			"conditions":                knativeReadyConditions(true),
			"latestReadyRevisionName":   shortLatest,
			"latestCreatedRevisionName": shortLatest,
			"url":                       svc.URI,
			"address":                   map[string]any{"url": svc.URI},
			"traffic":                   trafficStatus,
		},
	}
}

func toKnativeRevision(rev store.RunRevision, project, serviceID string) map[string]any {
	tplSpec := knativePodSpecFromTemplateJSON(rev.TemplateJSON)
	short := shortRevisionName(rev.Name)
	labels := map[string]any{}
	if serviceID != "" {
		labels[knativeServiceLabel] = serviceID
	}
	return map[string]any{
		"apiVersion": knativeAPIVersion,
		"kind":       "Revision",
		"metadata": map[string]any{
			"name":              short,
			"namespace":         project,
			"uid":               fmt.Sprintf("%s-%05d", short, rev.Generation),
			"generation":        rev.Generation,
			"creationTimestamp": rev.CreatedAt,
			"labels":            labels,
		},
		"spec": tplSpec,
		"status": map[string]any{
			"observedGeneration": rev.Generation,
			"conditions":         knativeReadyConditions(true),
			"imageDigest":        firstImageFromPodSpec(tplSpec),
		},
	}
}

func toKnativeConfiguration(svc store.RunService) map[string]any {
	tplSpec := knativePodSpecFromTemplateJSON(svc.TemplateJSON)
	shortLatest := shortRevisionName(svc.LatestRevision)
	return map[string]any{
		"apiVersion": knativeAPIVersion,
		"kind":       "Configuration",
		"metadata": map[string]any{
			"name":              svc.ServiceID,
			"namespace":         svc.ProjectID,
			"uid":               svc.UID + "-cfg",
			"generation":        svc.Generation,
			"creationTimestamp": svc.CreatedAt,
			"labels": map[string]any{
				knativeServiceLabel: svc.ServiceID,
			},
		},
		"spec": map[string]any{
			"template": map[string]any{
				"metadata": map[string]any{},
				"spec":     tplSpec,
			},
		},
		"status": map[string]any{
			"observedGeneration":        svc.Generation,
			"conditions":                knativeReadyConditions(true),
			"latestReadyRevisionName":   shortLatest,
			"latestCreatedRevisionName": shortLatest,
		},
	}
}

func toKnativeRoute(svc store.RunService) map[string]any {
	shortLatest := shortRevisionName(svc.LatestRevision)
	_, trafficStatus := knativeTrafficFromJSON(svc.TrafficJSON, shortLatest)
	return map[string]any{
		"apiVersion": knativeAPIVersion,
		"kind":       "Route",
		"metadata": map[string]any{
			"name":              svc.ServiceID,
			"namespace":         svc.ProjectID,
			"uid":               svc.UID + "-route",
			"generation":        svc.Generation,
			"creationTimestamp": svc.CreatedAt,
			"labels": map[string]any{
				knativeServiceLabel: svc.ServiceID,
			},
		},
		"spec": map[string]any{
			"traffic": []map[string]any{
				{"configurationName": svc.ServiceID, "latestRevision": true, "percent": 100},
			},
		},
		"status": map[string]any{
			"observedGeneration": svc.Generation,
			"conditions":         knativeReadyConditions(true),
			"url":                svc.URI,
			"address":            map[string]any{"url": svc.URI},
			"traffic":            trafficStatus,
		},
	}
}

func knativeReadyConditions(ready bool) []map[string]any {
	status := "False"
	if ready {
		status = "True"
	}
	return []map[string]any{
		{"type": "Ready", "status": status, "reason": "LabReady", "message": "lab resource ready"},
		{"type": "ConfigurationsReady", "status": status},
		{"type": "RoutesReady", "status": status},
	}
}

func knativePodSpecFromTemplateJSON(templateJSON string) map[string]any {
	var tpl map[string]any
	_ = json.Unmarshal([]byte(templateJSON), &tpl)
	if tpl == nil {
		tpl = map[string]any{}
	}
	out := map[string]any{
		"containers": containersFromTemplate(tpl),
	}
	if sa, _ := tpl["serviceAccount"].(string); sa != "" {
		out["serviceAccountName"] = sa
	}
	if sa, _ := tpl["serviceAccountName"].(string); sa != "" {
		out["serviceAccountName"] = sa
	}
	return out
}

func firstImageFromPodSpec(spec map[string]any) string {
	containers, _ := spec["containers"].([]any)
	if len(containers) == 0 {
		return ""
	}
	cm, _ := containers[0].(map[string]any)
	img, _ := cm["image"].(string)
	return img
}

func knativeTrafficFromJSON(trafficJSON, shortLatest string) (spec []map[string]any, status []map[string]any) {
	var raw []any
	if trafficJSON != "" {
		_ = json.Unmarshal([]byte(trafficJSON), &raw)
	}
	if len(raw) == 0 {
		spec = []map[string]any{{"latestRevision": true, "percent": 100}}
		status = []map[string]any{{"revisionName": shortLatest, "latestRevision": true, "percent": 100}}
		return spec, status
	}
	for _, item := range raw {
		m, _ := item.(map[string]any)
		if m == nil {
			continue
		}
		percent := 100
		if n, ok := asInt(m["percent"]); ok {
			percent = n
		}
		typ, _ := m["type"].(string)
		rev, _ := m["revision"].(string)
		revName, _ := m["revisionName"].(string)
		if revName == "" {
			revName = shortRevisionName(rev)
		}
		latest := typ == "TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST" || revName == "" || revName == shortLatest
		if b, ok := m["latestRevision"].(bool); ok {
			latest = b
		}
		entrySpec := map[string]any{"percent": percent}
		entryStatus := map[string]any{"percent": percent}
		if latest {
			entrySpec["latestRevision"] = true
			entryStatus["latestRevision"] = true
			entryStatus["revisionName"] = shortLatest
		} else {
			entrySpec["revisionName"] = revName
			entryStatus["revisionName"] = revName
		}
		spec = append(spec, entrySpec)
		status = append(status, entryStatus)
	}
	return spec, status
}

func shortRevisionName(full string) string {
	if full == "" {
		return ""
	}
	if i := strings.LastIndex(full, "/"); i >= 0 {
		return full[i+1:]
	}
	return full
}

func serviceIDFromServiceName(name string) string {
	// projects/{p}/locations/{loc}/services/{id}
	parts := strings.Split(name, "/")
	if len(parts) >= 6 && parts[4] == "services" {
		return parts[5]
	}
	return ""
}

func parseKnativeServiceLabelSelector(sel string) string {
	sel = strings.TrimSpace(sel)
	if sel == "" {
		return ""
	}
	for _, part := range strings.Split(sel, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, knativeServiceLabel+"=") {
			return strings.TrimPrefix(part, knativeServiceLabel+"=")
		}
	}
	return ""
}

func decodeKnativeServiceBody(r *http.Request) (map[string]any, error) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("invalid JSON body")
	}
	if body == nil {
		body = map[string]any{}
	}
	return body, nil
}

func knativeMetadataName(body map[string]any) string {
	md, _ := body["metadata"].(map[string]any)
	if md == nil {
		return ""
	}
	name, _ := md["name"].(string)
	return name
}

func knativeBodyToV2Template(body map[string]any) map[string]any {
	spec, _ := body["spec"].(map[string]any)
	if spec == nil {
		return map[string]any{}
	}
	template, _ := spec["template"].(map[string]any)
	if template == nil {
		return map[string]any{}
	}
	pod, _ := template["spec"].(map[string]any)
	if pod == nil {
		pod = map[string]any{}
	}
	out := map[string]any{}
	if c, ok := pod["containers"]; ok {
		out["containers"] = c
	}
	if sa, _ := pod["serviceAccountName"].(string); sa != "" {
		out["serviceAccount"] = sa
	}
	// Preserve lab theatre fields if callers put them on template metadata annotations or pod.
	if md, _ := template["metadata"].(map[string]any); md != nil {
		if ann, _ := md["annotations"].(map[string]any); ann != nil {
			if v, _ := ann["labResponseBody"].(string); v != "" {
				out["labResponseBody"] = v
			}
		}
	}
	for _, key := range []string{"labResponseBody", "labStatusCode", "labDelayMs"} {
		if v, ok := pod[key]; ok {
			out[key] = v
		}
	}
	return out
}

func knativeBodyToTrafficJSON(body map[string]any, shortLatest string) string {
	spec, _ := body["spec"].(map[string]any)
	if spec == nil {
		return ""
	}
	traffic, ok := spec["traffic"].([]any)
	if !ok || len(traffic) == 0 {
		return ""
	}
	out := make([]map[string]any, 0, len(traffic))
	for _, item := range traffic {
		m, _ := item.(map[string]any)
		if m == nil {
			continue
		}
		percent := 100
		if n, ok := asInt(m["percent"]); ok {
			percent = n
		}
		entry := map[string]any{"percent": percent}
		latest, _ := m["latestRevision"].(bool)
		revName, _ := m["revisionName"].(string)
		if latest || revName == "" {
			entry["type"] = "TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST"
			if shortLatest != "" {
				entry["revision"] = shortLatest
			}
		} else {
			entry["type"] = "TRAFFIC_TARGET_ALLOCATION_TYPE_REVISION"
			entry["revision"] = revName
		}
		out = append(out, entry)
	}
	raw, _ := json.Marshal(out)
	return string(raw)
}
