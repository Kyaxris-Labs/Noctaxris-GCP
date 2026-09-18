package containeranalysis

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
	"github.com/google/uuid"
)

// Service serves Container Analysis REST v1 (occurrence read/create lite).
type Service struct {
	Store *store.Store
	Authz *authz.Evaluator
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// Mount registers Container Analysis v1 REST routes.
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	mux.HandleFunc("GET /v1/projects/{project}/occurrences", s.wrap(principalFrom, s.listOccurrences))
	mux.HandleFunc("POST /v1/projects/{project}/occurrences", s.wrap(principalFrom, s.createOccurrence))
	mux.HandleFunc("GET /v1/projects/{project}/occurrences/{occurrence}", s.wrap(principalFrom, s.getOccurrence))
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

func writeAuthzErr(w http.ResponseWriter, err error) {
	if err == errDenied {
		gcperrors.PermissionDenied(w, "")
		return
	}
	gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
}

func occurrenceJSON(o store.ContainerOccurrence) map[string]any {
	var body map[string]any
	_ = json.Unmarshal([]byte(o.BodyJSON), &body)
	if body == nil {
		body = map[string]any{}
	}
	out := map[string]any{
		"name":        o.Name,
		"resourceUri": o.ResourceURI,
		"kind":        o.Kind,
		"noteName":    o.NoteName,
		"createTime":  o.CreatedAt,
	}
	for k, v := range body {
		if k == "name" {
			continue
		}
		out[k] = v
	}
	return out
}

func filterResourceURI(filter string) string {
	filter = strings.TrimSpace(filter)
	const needle = `resourceUrl="`
	i := strings.Index(filter, needle)
	if i < 0 {
		i = strings.Index(filter, `resourceUri="`)
		if i < 0 {
			return ""
		}
		rest := filter[i+len(`resourceUri="`):]
		end := strings.IndexByte(rest, '"')
		if end < 0 {
			return ""
		}
		return rest[:end]
	}
	rest := filter[i+len(needle):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return ""
	}
	return rest[:end]
}

func (s *Service) listOccurrences(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "containeranalysis.occurrences.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	uri := filterResourceURI(r.URL.Query().Get("filter"))
	list, err := s.Store.ListContainerOccurrences(project, uri)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for i := range list {
		items = append(items, occurrenceJSON(list[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"occurrences": items})
}

func (s *Service) getOccurrence(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "containeranalysis.occurrences.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := "projects/" + project + "/occurrences/" + r.PathValue("occurrence")
	o, ok, err := s.Store.GetContainerOccurrence(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "occurrence not found")
		return
	}
	writeJSON(w, http.StatusOK, occurrenceJSON(*o))
}

func (s *Service) createOccurrence(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "containeranalysis.occurrences.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body == nil {
		body = map[string]any{}
	}
	id := r.URL.Query().Get("occurrenceId")
	if id == "" {
		id, _ = body["name"].(string)
		if i := strings.LastIndex(id, "/"); i >= 0 {
			id = id[i+1:]
		}
	}
	if id == "" {
		id = uuid.NewString()
	}
	uri, _ := body["resourceUri"].(string)
	if uri == "" {
		uri, _ = body["resourceUrl"].(string)
	}
	kind, _ := body["kind"].(string)
	if kind == "" {
		kind = "ATTESTATION"
	}
	note, _ := body["noteName"].(string)
	raw, _ := json.Marshal(body)
	name := "projects/" + project + "/occurrences/" + id
	o := store.ContainerOccurrence{
		Name: name, ProjectID: project, OccurrenceID: id,
		ResourceURI: uri, Kind: kind, NoteName: note, BodyJSON: string(raw),
	}
	if err := s.Store.PutContainerOccurrence(o); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	got, ok, err := s.Store.GetContainerOccurrence(name)
	if err != nil || !ok {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "created occurrence missing")
		return
	}
	writeJSON(w, http.StatusOK, occurrenceJSON(*got))
}
