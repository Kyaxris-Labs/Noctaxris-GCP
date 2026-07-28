package eventarc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/httpegress"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// Service serves Eventarc Triggers REST v1 (lab subset).
type Service struct {
	Store *store.Store
	Authz *authz.Evaluator
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// Mount registers Eventarc channel routes. Regional triggers are mounted by
// server.registerLocationTriggers (shared with Cloud Build to avoid ServeMux clash).
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	mux.HandleFunc("POST /v1/projects/{project}/locations/{location}/channels", s.wrap(principalFrom, s.createChannel))
	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/channels", s.wrap(principalFrom, s.listChannels))
	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/channels/{channel}", s.wrap(principalFrom, s.getChannel))
	mux.HandleFunc("DELETE /v1/projects/{project}/locations/{location}/channels/{channel}", s.wrap(principalFrom, s.deleteChannel))
}

// CreateTriggerHTTP handles Eventarc trigger create (exported for shared regional mux).
func (s *Service) CreateTriggerHTTP(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	s.createTrigger(w, r, p)
}

// ListTriggersHTTP handles Eventarc trigger list.
func (s *Service) ListTriggersHTTP(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	s.listTriggers(w, r, p)
}

// GetTriggerHTTP handles Eventarc trigger get. Returns false when not found (no write).
func (s *Service) GetTriggerHTTP(w http.ResponseWriter, r *http.Request, p authn.Principal) bool {
	project, location, id := r.PathValue("project"), r.PathValue("location"), r.PathValue("trigger")
	name := fmt.Sprintf("projects/%s/locations/%s/triggers/%s", project, location, id)
	t, ok, err := s.Store.GetEventarcTrigger(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return true
	}
	if !ok {
		return false
	}
	if err := s.require(p, "eventarc.triggers.get", project); err != nil {
		writeAuthz(w, err)
		return true
	}
	writeJSON(w, http.StatusOK, triggerResource(t))
	return true
}

// DeleteTriggerHTTP handles Eventarc trigger delete. Returns false when not found.
func (s *Service) DeleteTriggerHTTP(w http.ResponseWriter, r *http.Request, p authn.Principal) bool {
	project, location, id := r.PathValue("project"), r.PathValue("location"), r.PathValue("trigger")
	name := fmt.Sprintf("projects/%s/locations/%s/triggers/%s", project, location, id)
	t, ok, err := s.Store.GetEventarcTrigger(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return true
	}
	if !ok {
		return false
	}
	_ = t
	if err := s.require(p, "eventarc.triggers.delete", project); err != nil {
		writeAuthz(w, err)
		return true
	}
	ok, err = s.Store.DeleteEventarcTrigger(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return true
	}
	if !ok {
		return false
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("{}"))
	return true
}

// LooksLikeEventarcTrigger reports Eventarc-shaped create bodies.
func LooksLikeEventarcTrigger(body map[string]any) bool {
	if body == nil {
		return false
	}
	for _, k := range []string{"eventFilters", "destination", "transport", "channel"} {
		if _, ok := body[k]; ok {
			return true
		}
	}
	return false
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
	out := map[string]any{
		"name":         t.Name,
		"uid":          t.TriggerID,
		"createTime":   t.CreatedAt,
		"eventFilters": filters,
		"destination":  dest,
		"transport":    transport,
	}
	if t.Channel != "" {
		out["channel"] = t.Channel
	}
	return out
}

// TriggerResourceJSON exports trigger JSON for the shared regional list mux.
func TriggerResourceJSON(t *store.EventarcTrigger) map[string]any {
	return triggerResource(t)
}

// MayListTriggers reports whether p may list Eventarc triggers in project.
func (s *Service) MayListTriggers(p authn.Principal, projectID string) bool {
	return s.require(p, "eventarc.triggers.list", projectID) == nil
}

func channelResource(c *store.EventarcChannel) map[string]any {
	return map[string]any{
		"name":        c.Name,
		"uid":         c.UID,
		"createTime":  c.CreatedAt,
		"provider":    c.Provider,
		"pubsubTopic": c.PubsubTopic,
		"state":       c.State,
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
		Channel      string          `json:"channel"`
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
	// Validate supported event types; allow extra attribute filters / values maps.
	var filters []struct {
		Attribute string            `json:"attribute"`
		Value     string            `json:"value"`
		Values    map[string]string `json:"values"`
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
	var destProbe struct {
		HTTPEndpoint *struct {
			URI string `json:"uri"`
		} `json:"httpEndpoint"`
	}
	_ = json.Unmarshal([]byte(destJSON), &destProbe)
	if destProbe.HTTPEndpoint != nil && strings.TrimSpace(destProbe.HTTPEndpoint.URI) != "" {
		if err := httpegress.Validate(destProbe.HTTPEndpoint.URI); err != nil {
			gcperrors.InvalidArgument(w, err.Error())
			return
		}
	}
	t, created, err := s.Store.CreateEventarcTrigger(store.EventarcTrigger{
		ProjectID: project, Location: location, TriggerID: triggerID,
		FiltersJSON: filtersJSON, DestinationJSON: destJSON, TransportJSON: transportJSON, Channel: body.Channel,
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

func (s *Service) createChannel(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project, location := r.PathValue("project"), r.PathValue("location")
	if err := s.require(p, "eventarc.channels.create", project); err != nil {
		writeAuthz(w, err)
		return
	}
	channelID := r.URL.Query().Get("channelId")
	var body struct {
		Name        string `json:"name"`
		Provider    string `json:"provider"`
		PubsubTopic string `json:"pubsubTopic"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if channelID == "" && body.Name != "" {
		parts := strings.Split(body.Name, "/")
		channelID = parts[len(parts)-1]
	}
	if channelID == "" {
		gcperrors.InvalidArgument(w, "channelId query parameter or name is required")
		return
	}
	c, created, err := s.Store.CreateEventarcChannel(store.EventarcChannel{
		ProjectID: project, Location: location, ChannelID: channelID,
		Provider: body.Provider, PubsubTopic: body.PubsubTopic,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "channel already exists")
		return
	}
	writeJSON(w, http.StatusOK, channelResource(c))
}

func (s *Service) getChannel(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project, location, channel := r.PathValue("project"), r.PathValue("location"), r.PathValue("channel")
	if err := s.require(p, "eventarc.channels.get", project); err != nil {
		writeAuthz(w, err)
		return
	}
	name := "projects/" + project + "/locations/" + location + "/channels/" + channel
	c, ok, err := s.Store.GetEventarcChannel(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "channel not found")
		return
	}
	writeJSON(w, http.StatusOK, channelResource(c))
}

func (s *Service) listChannels(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project, location := r.PathValue("project"), r.PathValue("location")
	if err := s.require(p, "eventarc.channels.list", project); err != nil {
		writeAuthz(w, err)
		return
	}
	list, err := s.Store.ListEventarcChannels(project, location)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(list))
	for i := range list {
		out = append(out, channelResource(&list[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"channels": out})
}

func (s *Service) deleteChannel(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project, location, channel := r.PathValue("project"), r.PathValue("location"), r.PathValue("channel")
	if err := s.require(p, "eventarc.channels.delete", project); err != nil {
		writeAuthz(w, err)
		return
	}
	name := "projects/" + project + "/locations/" + location + "/channels/" + channel
	ok, err := s.Store.DeleteEventarcChannel(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "channel not found")
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("{}"))
}
