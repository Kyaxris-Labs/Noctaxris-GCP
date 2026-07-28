package eventarc

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

// Service serves Eventarc Triggers REST v1 (lab subset).
type Service struct {
	Store *store.Store
	Authz *authz.Evaluator
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// Mount registers Eventarc trigger routes.
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	mux.HandleFunc("POST /v1/projects/{project}/locations/{location}/triggers", s.wrap(principalFrom, s.createTrigger))
	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/triggers", s.wrap(principalFrom, s.listTriggers))
	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/triggers/{trigger}", s.wrap(principalFrom, s.getTrigger))
	mux.HandleFunc("DELETE /v1/projects/{project}/locations/{location}/triggers/{trigger}", s.wrap(principalFrom, s.deleteTrigger))
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

func triggerResource(t *store.EventarcTrigger) map[string]any {
	var filters any
	_ = json.Unmarshal([]byte(t.FiltersJSON), &filters)
	var dest any
	_ = json.Unmarshal([]byte(t.DestinationJSON), &dest)
	var transport any
	_ = json.Unmarshal([]byte(t.TransportJSON), &transport)
	return map[string]any{
		"name":         t.Name,
		"uid":          t.TriggerID,
		"createTime":   t.CreatedAt,
		"eventFilters": filters,
		"destination":  dest,
		"transport":    transport,
	}
}

func (s *Service) createTrigger(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project, location := r.PathValue("project"), r.PathValue("location")
	if err := s.require(p, "eventarc.triggers.create", project); err != nil {
		writeAuthz(w, err)
		return
	}
	triggerID := r.URL.Query().Get("triggerId")
	var body struct {
		Name         string          `json:"name"`
		EventFilters json.RawMessage `json:"eventFilters"`
		Destination  json.RawMessage `json:"destination"`
		Transport    json.RawMessage `json:"transport"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	if triggerID == "" && body.Name != "" {
		parts := strings.Split(body.Name, "/")
		if len(parts) > 0 {
			triggerID = parts[len(parts)-1]
		}
	}
	if triggerID == "" {
		gcperrors.InvalidArgument(w, "triggerId query parameter or name is required")
		return
	}
	filtersJSON := "[]"
	if len(body.EventFilters) > 0 {
		filtersJSON = string(body.EventFilters)
	}
	destJSON := "{}"
	if len(body.Destination) > 0 {
		destJSON = string(body.Destination)
	}
	transportJSON := "{}"
	if len(body.Transport) > 0 {
		transportJSON = string(body.Transport)
	}
	// Validate supported event types.
	var filters []struct {
		Attribute string `json:"attribute"`
		Value     string `json:"value"`
	}
	_ = json.Unmarshal([]byte(filtersJSON), &filters)
	hasType := false
	for _, f := range filters {
		if f.Attribute == "type" {
			hasType = true
			switch f.Value {
			case "google.cloud.pubsub.topic.v1.messagePublished",
				"google.cloud.storage.object.v1.finalized":
			default:
				gcperrors.InvalidArgument(w, "supported event types: google.cloud.pubsub.topic.v1.messagePublished, google.cloud.storage.object.v1.finalized")
				return
			}
		}
	}
	if !hasType {
		gcperrors.InvalidArgument(w, "eventFilters must include type")
		return
	}
	t, created, err := s.Store.CreateEventarcTrigger(store.EventarcTrigger{
		ProjectID: project, Location: location, TriggerID: triggerID,
		FiltersJSON: filtersJSON, DestinationJSON: destJSON, TransportJSON: transportJSON,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "trigger already exists")
		return
	}
	writeJSON(w, http.StatusOK, triggerResource(t))
}

func (s *Service) getTrigger(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project, location, trigger := r.PathValue("project"), r.PathValue("location"), r.PathValue("trigger")
	if err := s.require(p, "eventarc.triggers.get", project); err != nil {
		writeAuthz(w, err)
		return
	}
	name := "projects/" + project + "/locations/" + location + "/triggers/" + trigger
	t, ok, err := s.Store.GetEventarcTrigger(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "trigger not found")
		return
	}
	writeJSON(w, http.StatusOK, triggerResource(t))
}

func (s *Service) listTriggers(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project, location := r.PathValue("project"), r.PathValue("location")
	if err := s.require(p, "eventarc.triggers.list", project); err != nil {
		writeAuthz(w, err)
		return
	}
	list, err := s.Store.ListEventarcTriggers(project, location)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(list))
	for i := range list {
		out = append(out, triggerResource(&list[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"triggers": out})
}

func (s *Service) deleteTrigger(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project, location, trigger := r.PathValue("project"), r.PathValue("location"), r.PathValue("trigger")
	if err := s.require(p, "eventarc.triggers.delete", project); err != nil {
		writeAuthz(w, err)
		return
	}
	name := "projects/" + project + "/locations/" + location + "/triggers/" + trigger
	ok, err := s.Store.DeleteEventarcTrigger(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "trigger not found")
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("{}"))
}

func writeAuthz(w http.ResponseWriter, err error) {
	if err == errDenied {
		gcperrors.PermissionDenied(w, "")
		return
	}
	gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
}
