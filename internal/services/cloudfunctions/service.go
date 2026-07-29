package cloudfunctions

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
	"github.com/google/uuid"
)

// DefaultLocation is the lab default Functions location.
const DefaultLocation = "us-central1"

const functionsUploadBucket = "noctaxris-gcp-functions-lab"

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
	mux.HandleFunc("POST /v2/projects/{project}/locations/{location}/functions:generateUploadUrl", s.wrap(principalFrom, s.generateUploadUrl))
	// Source upload accept theatre (signed-URL shaped path from generateUploadUrl).
	mux.HandleFunc("PUT /v2/projects/{project}/locations/{location}/functions:upload/{uploadId}", s.wrap(principalFrom, s.acceptUpload))
	mux.HandleFunc("POST /v2/projects/{project}/locations/{location}/functions:upload/{uploadId}", s.wrap(principalFrom, s.acceptUpload))
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

// requireAny allows when any listed resource (or its project parent chain) grants permission.
func (s *Service) requireAny(p authn.Principal, permission string, resources ...string) error {
	ok, err := s.Authz.EvaluateAny(p.Email, p.IsRoot, permission, resources...)
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
	state := "ACTIVE"
	if bucket, object, hasSrc := storageSourceFromBody(body); hasSrc {
		uploaded, err := s.Store.HasCloudFunctionUploadObject(project, bucket, object)
		if err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		}
		if !uploaded {
			state = "DEPLOYING"
		}
	}
	name := functionName(project, location, functionID)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	created, err := s.Store.CreateCloudFunction(store.CloudFunction{
		Name:            name,
		ProjectID:       project,
		Location:        location,
		FunctionID:      functionID,
		State:           state,
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
	if err := s.wireEventarcFromCreate(project, location, functionID, name, body); err != nil {
		_, _ = s.Store.DeleteCloudFunction(name)
		gcperrors.InvalidArgument(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toFunctionJSON(fn))
}

func (s *Service) generateUploadUrl(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	if err := s.require(p, "cloudfunctions.functions.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	id := uuid.NewString()
	object := fmt.Sprintf("uploads/%s/%s.zip", project, id)
	writeJSON(w, http.StatusOK, map[string]any{
		"uploadUrl": fmt.Sprintf("http://127.0.0.1:4588/v2/projects/%s/locations/%s/functions:upload/%s", project, location, id),
		"storageSource": map[string]any{
			"bucket": functionsUploadBucket,
			"object": object,
		},
	})
}

func (s *Service) acceptUpload(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	uploadID := r.PathValue("uploadId")
	if err := s.require(p, "cloudfunctions.functions.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	if uploadID == "" {
		gcperrors.InvalidArgument(w, "upload id is required")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		gcperrors.InvalidArgument(w, "failed to read upload body")
		return
	}
	object := fmt.Sprintf("uploads/%s/%s.zip", project, uploadID)
	if err := s.Store.AcceptCloudFunctionUpload(store.CloudFunctionUpload{
		UploadID:  uploadID,
		ProjectID: project,
		Location:  location,
		Bucket:    functionsUploadBucket,
		Object:    object,
		SizeBytes: int64(len(body)),
	}); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if _, err := s.Store.ActivateCloudFunctionsForStorageSource(project, location, functionsUploadBucket, object); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"bucket": functionsUploadBucket,
		"object": object,
		"size":   len(body),
	})
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
	switch action {
	case "invoke":
		s.invoke(w, r, p, project, location, id)
		return
	case "getIamPolicy":
		s.getIamPolicy(w, r, p, project, location, id)
		return
	case "setIamPolicy":
		s.setIamPolicy(w, r, p, project, location, id)
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
	existing, ok, err := s.Store.GetCloudFunction(functionName(project, location, id))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Function not found")
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	cfgRaw := existing.ConfigJSON
	labResp := ""
	if body != nil {
		var existingCfg map[string]any
		_ = json.Unmarshal([]byte(existing.ConfigJSON), &existingCfg)
		if existingCfg == nil {
			existingCfg = map[string]any{}
		}
		for k, v := range body {
			existingCfg[k] = v
		}
		b, _ := json.Marshal(existingCfg)
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
	// When patch supplies storageSource that was already uploaded, promote to ACTIVE.
	if bucket, object, hasSrc := storageSourceFromConfigJSON(fn.ConfigJSON); hasSrc {
		uploaded, err := s.Store.HasCloudFunctionUploadObject(project, bucket, object)
		if err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		}
		if uploaded && fn.State != "ACTIVE" {
			fn, ok, err = s.Store.SetCloudFunctionState(name, "ACTIVE")
			if err != nil {
				gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
				return
			}
			if !ok {
				gcperrors.NotFound(w, "Function not found")
				return
			}
		}
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

func (s *Service) getIamPolicy(w http.ResponseWriter, _ *http.Request, p authn.Principal, project, location, id string) {
	if err := s.require(p, "cloudfunctions.functions.getIamPolicy", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := functionName(project, location, id)
	if _, ok, err := s.Store.GetCloudFunction(name); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "Function not found")
		return
	}
	raw, found, err := s.Store.GetIAMPolicyJSON(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !found {
		writeJSON(w, http.StatusOK, authz.Policy{Etag: "ACAB", Bindings: []authz.Binding{}})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func (s *Service) setIamPolicy(w http.ResponseWriter, r *http.Request, p authn.Principal, project, location, id string) {
	if err := s.require(p, "cloudfunctions.functions.setIamPolicy", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := functionName(project, location, id)
	if _, ok, err := s.Store.GetCloudFunction(name); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "Function not found")
		return
	}
	var req struct {
		Policy authz.Policy `json:"policy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		gcperrors.InvalidArgument(w, "invalid setIamPolicy body")
		return
	}
	if err := s.Store.PutIAMPolicyJSON(name, req.Policy); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, req.Policy)
}

func (s *Service) invoke(w http.ResponseWriter, _ *http.Request, p authn.Principal, project, location, id string) {
	name := functionName(project, location, id)
	// Project binding or function-resource Invoker (roles/cloudfunctions.invoker).
	if err := s.requireAny(p, "cloudfunctions.functions.invoke", name, "projects/"+project); err != nil {
		writeAuthzErr(w, err)
		return
	}
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
		"name":        fn.Name,
		"state":       fn.State,
		"createTime":  fn.CreatedAt,
		"updateTime":  fn.UpdatedAt,
		"url":         fn.URI,
		"environment": "GEN_2",
	}
	if m, ok := cfg.(map[string]any); ok {
		if bc, ok := m["buildConfig"]; ok {
			out["buildConfig"] = bc
		}
		if sc, ok := m["serviceConfig"]; ok {
			out["serviceConfig"] = sc
		}
		if labels, ok := m["labels"]; ok {
			out["labels"] = labels
		}
		if desc, ok := m["description"]; ok {
			out["description"] = desc
		}
		if et := eventTriggerEcho(m, fn); et != nil {
			out["eventTrigger"] = et
		}
	}
	return out
}

// wireEventarcFromCreate inserts an Eventarc trigger when create includes
// eventTrigger / eventarcTrigger / Eventarc-shaped filters.
func (s *Service) wireEventarcFromCreate(project, location, functionID, functionName string, body map[string]any) error {
	filtersJSON, transportJSON, channel, triggerLoc, serviceAccount, present, err := extractEventTriggerSpec(body)
	if err != nil {
		return err
	}
	if !present {
		return nil
	}
	if triggerLoc == "" {
		triggerLoc = location
	}
	triggerID := "function-" + functionID
	destRaw, _ := json.Marshal(map[string]any{"cloudFunction": functionName})
	_, created, err := s.Store.CreateEventarcTrigger(store.EventarcTrigger{
		ProjectID:       project,
		Location:        triggerLoc,
		TriggerID:       triggerID,
		FiltersJSON:     filtersJSON,
		DestinationJSON: string(destRaw),
		TransportJSON:   transportJSON,
		Channel:         channel,
		ServiceAccount:  serviceAccount,
	})
	if err != nil {
		return fmt.Errorf("wire Eventarc trigger: %w", err)
	}
	if !created {
		return fmt.Errorf("Eventarc trigger %s already exists", triggerID)
	}
	return nil
}

func extractEventTriggerSpec(body map[string]any) (filtersJSON, transportJSON, channel, triggerRegion, serviceAccount string, present bool, err error) {
	if body == nil {
		return "", "", "", "", "", false, nil
	}
	var src map[string]any
	for _, key := range []string{"eventTrigger", "eventarcTrigger"} {
		if m, isMap := body[key].(map[string]any); isMap && m != nil {
			src = m
			break
		}
	}
	if src == nil {
		// Eventarc-shaped top-level fields (eventFilters / transport / channel) without destination.
		if _, has := body["eventFilters"]; !has {
			return "", "", "", "", "", false, nil
		}
		if _, hasDest := body["destination"]; hasDest {
			return "", "", "", "", "", false, nil
		}
		src = body
	}

	eventType, _ := src["eventType"].(string)
	if eventType == "" {
		eventType, _ = src["event_type"].(string)
	}
	var filters []map[string]any
	if raw, has := src["eventFilters"]; has {
		switch v := raw.(type) {
		case []any:
			for _, item := range v {
				if m, isMap := item.(map[string]any); isMap {
					filters = append(filters, m)
				}
			}
		case []map[string]any:
			filters = append(filters, v...)
		}
	}
	hasType := false
	for _, f := range filters {
		if attr, _ := f["attribute"].(string); attr == "type" {
			hasType = true
			break
		}
	}
	if !hasType && eventType != "" {
		filters = append([]map[string]any{{"attribute": "type", "value": eventType}}, filters...)
		hasType = true
	}
	if !hasType {
		return "", "", "", "", "", true, fmt.Errorf("eventTrigger requires eventType or eventFilters type")
	}
	for _, f := range filters {
		if attr, _ := f["attribute"].(string); attr != "type" {
			continue
		}
		val, _ := f["value"].(string)
		switch val {
		case "google.cloud.pubsub.topic.v1.messagePublished",
			"google.cloud.storage.object.v1.finalized":
		default:
			return "", "", "", "", "", true, fmt.Errorf("supported event types: google.cloud.pubsub.topic.v1.messagePublished, google.cloud.storage.object.v1.finalized")
		}
	}
	fb, _ := json.Marshal(filters)
	filtersJSON = string(fb)

	transportJSON = "{}"
	if t, isMap := src["transport"].(map[string]any); isMap {
		raw, _ := json.Marshal(t)
		transportJSON = string(raw)
	} else if topic, _ := src["pubsubTopic"].(string); topic != "" {
		raw, _ := json.Marshal(map[string]any{"pubsub": map[string]string{"topic": topic}})
		transportJSON = string(raw)
	} else if topic, _ := src["pubsub_topic"].(string); topic != "" {
		raw, _ := json.Marshal(map[string]any{"pubsub": map[string]string{"topic": topic}})
		transportJSON = string(raw)
	}

	channel, _ = src["channel"].(string)
	triggerRegion, _ = src["triggerRegion"].(string)
	if triggerRegion == "" {
		triggerRegion, _ = src["trigger_region"].(string)
	}
	serviceAccount, _ = src["serviceAccountEmail"].(string)
	if serviceAccount == "" {
		serviceAccount, _ = src["service_account_email"].(string)
	}
	return filtersJSON, transportJSON, channel, triggerRegion, serviceAccount, true, nil
}

func eventTriggerEcho(cfg map[string]any, fn store.CloudFunction) map[string]any {
	var src map[string]any
	for _, key := range []string{"eventTrigger", "eventarcTrigger"} {
		if m, ok := cfg[key].(map[string]any); ok && m != nil {
			src = m
			break
		}
	}
	if src == nil {
		return nil
	}
	out := map[string]any{}
	for k, v := range src {
		out[k] = v
	}
	loc := fn.Location
	if tr, ok := out["triggerRegion"].(string); ok && tr != "" {
		loc = tr
	} else if tr, ok := out["trigger_region"].(string); ok && tr != "" {
		loc = tr
	}
	out["trigger"] = fmt.Sprintf("projects/%s/locations/%s/triggers/function-%s",
		fn.ProjectID, loc, fn.FunctionID)
	return out
}

func storageSourceFromBody(body map[string]any) (bucket, object string, ok bool) {
	return storageSourceFromConfigJSON(mustJSON(body))
}

func mustJSON(body map[string]any) string {
	raw, _ := json.Marshal(body)
	return string(raw)
}

func storageSourceFromConfigJSON(configJSON string) (bucket, object string, ok bool) {
	var cfg map[string]any
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return "", "", false
	}
	bc, _ := cfg["buildConfig"].(map[string]any)
	if bc == nil {
		return "", "", false
	}
	src, _ := bc["source"].(map[string]any)
	if src == nil {
		return "", "", false
	}
	ss, _ := src["storageSource"].(map[string]any)
	if ss == nil {
		return "", "", false
	}
	bucket, _ = ss["bucket"].(string)
	object, _ = ss["object"].(string)
	return bucket, object, bucket != "" && object != ""
}
