package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/cloudbuild"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/eventarc"
)

// registerLocationTriggers mounts the shared regional triggers path used by both
// Eventarc and Cloud Build. Body shape selects the create handler; get/delete
// probe Eventarc then Cloud Build; list merges both inventories when authorized.
func (s *Server) registerLocationTriggers() {
	ea := &eventarc.Service{Store: s.store, Authz: s.authz}
	cb := &cloudbuild.Service{Store: s.store, Authz: s.authz}
	principalFrom := func(r *http.Request) (authn.Principal, bool) {
		return PrincipalFromContext(r.Context())
	}

	s.mux.HandleFunc("POST /v1/projects/{project}/locations/{location}/triggers", func(w http.ResponseWriter, r *http.Request) {
		p, ok := principalFrom(r)
		if !ok {
			gcperrors.Unauthenticated(w, "")
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			gcperrors.InvalidArgument(w, "invalid body")
			return
		}
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		r.Body = io.NopCloser(bytes.NewReader(raw))
		if eventarc.LooksLikeEventarcTrigger(body) {
			ea.CreateTriggerHTTP(w, r, p)
			return
		}
		cb.CreateTriggerHTTP(w, r, p)
	})

	s.mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/triggers", func(w http.ResponseWriter, r *http.Request) {
		p, ok := principalFrom(r)
		if !ok {
			gcperrors.Unauthenticated(w, "")
			return
		}
		project, location := r.PathValue("project"), r.PathValue("location")
		eaOK := ea.MayListTriggers(p, project)
		cbOK := cb.MayListTriggers(p, project)
		if !eaOK && !cbOK {
			gcperrors.PermissionDenied(w, "")
			return
		}
		items := make([]map[string]any, 0)
		if eaOK {
			list, err := s.store.ListEventarcTriggers(project, location)
			if err != nil {
				gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
				return
			}
			for i := range list {
				items = append(items, eventarc.TriggerResourceJSON(&list[i]))
			}
		}
		if cbOK {
			list, err := s.store.ListCbTriggers(project, location)
			if err != nil {
				gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
				return
			}
			for _, t := range list {
				items = append(items, cloudbuild.TriggerResourceJSON(t))
			}
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"triggers": items})
	})

	s.mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/triggers/{trigger}", func(w http.ResponseWriter, r *http.Request) {
		p, ok := principalFrom(r)
		if !ok {
			gcperrors.Unauthenticated(w, "")
			return
		}
		if ea.GetTriggerHTTP(w, r, p) {
			return
		}
		if cb.GetTriggerHTTP(w, r, p) {
			return
		}
		gcperrors.NotFound(w, "trigger not found")
	})

	s.mux.HandleFunc("DELETE /v1/projects/{project}/locations/{location}/triggers/{trigger}", func(w http.ResponseWriter, r *http.Request) {
		p, ok := principalFrom(r)
		if !ok {
			gcperrors.Unauthenticated(w, "")
			return
		}
		if ea.DeleteTriggerHTTP(w, r, p) {
			return
		}
		if cb.DeleteTriggerHTTP(w, r, p) {
			return
		}
		gcperrors.NotFound(w, "trigger not found")
	})

	s.mux.HandleFunc("POST /v1/projects/{project}/locations/{location}/triggers/{trigger}", func(w http.ResponseWriter, r *http.Request) {
		p, ok := principalFrom(r)
		if !ok {
			gcperrors.Unauthenticated(w, "")
			return
		}
		cb.TriggerPOSTActionHTTP(w, r, p)
	})
}
