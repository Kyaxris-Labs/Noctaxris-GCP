package containeranalysis

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
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
	mux.HandleFunc("GET /v1/projects/{project}/occurrences:vulnerabilitySummary", s.wrap(principalFrom, s.vulnerabilitySummary))
	mux.HandleFunc("GET /v1/projects/{project}/occurrences/{occurrence}", s.wrap(principalFrom, s.getOccurrence))

	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/occurrences", s.wrap(principalFrom, s.listOccurrences))
	mux.HandleFunc("POST /v1/projects/{project}/locations/{location}/occurrences", s.wrap(principalFrom, s.createOccurrence))
	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/occurrences:vulnerabilitySummary", s.wrap(principalFrom, s.vulnerabilitySummary))
	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/occurrences/{occurrence}", s.wrap(principalFrom, s.getOccurrence))
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
	// Emit the Artifact Registry / gcloud attach form so
	// `gcloud artifacts docker images list --show-occurrences` can key
	// metadata by https://…@sha256-<hex> (see containeranalysis_util.py).
	uri := gcloudArtifactsResourceURI(o.ResourceURI)
	out := map[string]any{
		"name":        o.Name,
		"resourceUri": uri,
		"kind":        o.Kind,
		"noteName":    o.NoteName,
		"createTime":  o.CreatedAt,
	}
	for k, v := range body {
		if k == "name" || k == "resourceUri" || k == "resourceUrl" {
			continue
		}
		out[k] = v
	}
	return out
}

// gcloudArtifactsResourceURI rewrites a stored resource URI into the form
// gcloud uses when attaching occurrences to AR docker image list rows:
// https-prefixed and @sha256:<hex> converted to @sha256-<hex>.
func gcloudArtifactsResourceURI(uri string) string {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return uri
	}
	bare := uri
	lower := strings.ToLower(bare)
	switch {
	case strings.HasPrefix(lower, "https://"):
		bare = bare[len("https://"):]
	case strings.HasPrefix(lower, "http://"):
		bare = bare[len("http://"):]
	}
	if i := strings.Index(bare, "@sha256:"); i >= 0 {
		bare = bare[:i] + "@sha256-" + bare[i+len("@sha256:"):]
	}
	return "https://" + bare
}

func parsePageParams(r *http.Request) (pageSize, offset int, err error) {
	pageSize = 100
	if raw := strings.TrimSpace(r.URL.Query().Get("pageSize")); raw != "" {
		n, convErr := strconv.Atoi(raw)
		if convErr != nil || n < 0 {
			return 0, 0, fmt.Errorf("invalid pageSize")
		}
		pageSize = n
	}
	if pageSize <= 0 {
		pageSize = 100
	}
	if pageSize > 1000 {
		pageSize = 1000
	}
	if tok := strings.TrimSpace(r.URL.Query().Get("pageToken")); tok != "" {
		n, convErr := strconv.Atoi(tok)
		if convErr != nil || n < 0 {
			return 0, 0, fmt.Errorf("invalid pageToken")
		}
		offset = n
	}
	return pageSize, offset, nil
}

func paginateOccurrences(list []store.ContainerOccurrence, pageSize, offset int) (page []store.ContainerOccurrence, next string) {
	if offset > len(list) {
		offset = len(list)
	}
	end := offset + pageSize
	if end > len(list) {
		end = len(list)
	}
	page = list[offset:end]
	if end < len(list) {
		next = strconv.Itoa(end)
	}
	return page, next
}

func (s *Service) filteredOccurrences(project, filterRaw string) ([]store.ContainerOccurrence, error) {
	f, err := parseOccurrenceFilter(filterRaw)
	if err != nil {
		return nil, err
	}
	// Empty resourceURI loads the full project set; BinaryAuthz keeps exact-match
	// ListContainerOccurrences(project, imageURI) for admit checks.
	list, err := s.Store.ListContainerOccurrences(project, "")
	if err != nil {
		return nil, err
	}
	return applyFilter(list, f), nil
}

func (s *Service) listOccurrences(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "containeranalysis.occurrences.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	pageSize, offset, err := parsePageParams(r)
	if err != nil {
		gcperrors.InvalidArgument(w, err.Error())
		return
	}
	list, err := s.filteredOccurrences(project, r.URL.Query().Get("filter"))
	if err != nil {
		if strings.Contains(err.Error(), "filter") || strings.Contains(err.Error(), "expected") ||
			strings.Contains(err.Error(), "unsupported") || strings.Contains(err.Error(), "unterminated") ||
			strings.Contains(err.Error(), "missing") || strings.Contains(err.Error(), "trailing") {
			gcperrors.InvalidArgument(w, err.Error())
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	page, next := paginateOccurrences(list, pageSize, offset)
	items := make([]map[string]any, 0, len(page))
	for i := range page {
		items = append(items, occurrenceJSON(page[i]))
	}
	resp := map[string]any{"occurrences": items}
	if next != "" {
		resp["nextPageToken"] = next
	}
	writeJSON(w, http.StatusOK, resp)
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

func (s *Service) vulnerabilitySummary(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "containeranalysis.occurrences.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	list, err := s.filteredOccurrences(project, r.URL.Query().Get("filter"))
	if err != nil {
		if strings.Contains(err.Error(), "filter") || strings.Contains(err.Error(), "expected") ||
			strings.Contains(err.Error(), "unsupported") || strings.Contains(err.Error(), "unterminated") ||
			strings.Contains(err.Error(), "missing") || strings.Contains(err.Error(), "trailing") {
			gcperrors.InvalidArgument(w, err.Error())
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"counts": buildVulnerabilityCounts(list)})
}

type severityBucket struct {
	total   int64
	fixable int64
}

func buildVulnerabilityCounts(list []store.ContainerOccurrence) []map[string]any {
	// resourceUri -> severity -> counts. Unknown severity rolls into totals only;
	// SEVERITY_UNSPECIFIED in the response is the per-resource total row.
	byURI := map[string]map[string]*severityBucket{}
	uriOrder := []string{}
	for i := range list {
		o := list[i]
		if !strings.EqualFold(o.Kind, "VULNERABILITY") {
			continue
		}
		uri := o.ResourceURI
		if _, ok := byURI[uri]; !ok {
			byURI[uri] = map[string]*severityBucket{}
			uriOrder = append(uriOrder, uri)
		}
		sev := occurrenceSeverity(o)
		key := sev
		if key == "SEVERITY_UNSPECIFIED" {
			key = "_unknown"
		}
		b := byURI[uri][key]
		if b == nil {
			b = &severityBucket{}
			byURI[uri][key] = b
		}
		b.total++
		if occurrenceFixable(o) {
			b.fixable++
		}
	}

	sevOrder := []string{"CRITICAL", "HIGH", "MEDIUM", "LOW", "MINIMAL"}
	out := make([]map[string]any, 0)
	for _, uri := range uriOrder {
		sevs := byURI[uri]
		var totalAll, fixableAll int64
		for _, b := range sevs {
			totalAll += b.total
			fixableAll += b.fixable
		}
		for _, sev := range sevOrder {
			b := sevs[sev]
			if b == nil || b.total == 0 {
				continue
			}
			out = append(out, countRow(uri, sev, b.total, b.fixable))
		}
		out = append(out, countRow(uri, "SEVERITY_UNSPECIFIED", totalAll, fixableAll))
	}
	return out
}

func countRow(uri, severity string, total, fixable int64) map[string]any {
	return map[string]any{
		"resourceUri":   gcloudArtifactsResourceURI(uri),
		"severity":      severity,
		"totalCount":    strconv.FormatInt(total, 10),
		"fixableCount": strconv.FormatInt(fixable, 10),
	}
}

func occurrenceSeverity(o store.ContainerOccurrence) string {
	var body map[string]any
	_ = json.Unmarshal([]byte(o.BodyJSON), &body)
	if body == nil {
		return "SEVERITY_UNSPECIFIED"
	}
	if v, ok := body["vulnerability"].(map[string]any); ok {
		for _, key := range []string{"effectiveSeverity", "severity"} {
			if s, ok := v[key].(string); ok && strings.TrimSpace(s) != "" {
				return normalizeSeverity(s)
			}
		}
	}
	if s, ok := body["severity"].(string); ok && strings.TrimSpace(s) != "" {
		return normalizeSeverity(s)
	}
	return "SEVERITY_UNSPECIFIED"
}

func normalizeSeverity(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "SEVERITY_")
	switch s {
	case "CRITICAL", "HIGH", "MEDIUM", "LOW", "MINIMAL":
		return s
	case "UNSPECIFIED", "":
		return "SEVERITY_UNSPECIFIED"
	default:
		return "SEVERITY_UNSPECIFIED"
	}
}

func occurrenceFixable(o store.ContainerOccurrence) bool {
	var body map[string]any
	_ = json.Unmarshal([]byte(o.BodyJSON), &body)
	if body == nil {
		return false
	}
	v, ok := body["vulnerability"].(map[string]any)
	if !ok {
		return false
	}
	if b, ok := v["fixAvailable"].(bool); ok {
		return b
	}
	if details, ok := v["packageIssue"].([]any); ok {
		for _, d := range details {
			m, ok := d.(map[string]any)
			if !ok {
				continue
			}
			if fixed, ok := m["fixedVersion"].(map[string]any); ok && len(fixed) > 0 {
				return true
			}
			if s, ok := m["fixedCpeUri"].(string); ok && s != "" {
				return true
			}
		}
	}
	return false
}
