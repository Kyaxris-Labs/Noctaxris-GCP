package cloudasset

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/restlab"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
	"github.com/google/uuid"
)

// Service serves Cloud Asset Inventory v1 REST (search / list / export lite).
type Service struct {
	Store *store.Store
	Authz *authz.Evaluator
}

type principalFunc = restlab.PrincipalFrom

// Mount registers Cloud Asset Inventory REST routes.
// Colon custom methods live in the last path segment (ServeMux cannot embed ':').
// Project-scoped POST :exportAssets is also reachable via CRM's POST /v1/projects/{project}
// dispatcher (HandleProjectExport) because that pattern is already owned by getAncestry.
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	mux.HandleFunc("GET /v1/projects/{project}", restlab.Wrap(principalFrom, s.projectGETColon))
	mux.HandleFunc("GET /v1/folders/{folder}", restlab.Wrap(principalFrom, s.folderGETColon))
	mux.HandleFunc("GET /v1/organizations/{organization}", restlab.Wrap(principalFrom, s.orgGETColon))

	mux.HandleFunc("GET /v1/projects/{project}/assets", restlab.Wrap(principalFrom, s.listAssets))
	mux.HandleFunc("GET /v1/folders/{folder}/assets", restlab.Wrap(principalFrom, s.listAssetsFolder))
	mux.HandleFunc("GET /v1/organizations/{organization}/assets", restlab.Wrap(principalFrom, s.listAssetsOrg))

	mux.HandleFunc("POST /v1/folders/{folder}", restlab.Wrap(principalFrom, s.folderPOSTColon))
	mux.HandleFunc("POST /v1/organizations/{organization}", restlab.Wrap(principalFrom, s.orgPOSTColon))

	mux.HandleFunc("POST /v1/projects/{project}/feeds", restlab.Wrap(principalFrom, s.createFeed))
	mux.HandleFunc("GET /v1/projects/{project}/feeds", restlab.Wrap(principalFrom, s.listFeeds))
	mux.HandleFunc("GET /v1/projects/{project}/feeds/{feed}", restlab.Wrap(principalFrom, s.getFeed))
	mux.HandleFunc("DELETE /v1/projects/{project}/feeds/{feed}", restlab.Wrap(principalFrom, s.deleteFeed))
}

// HandleProjectExport serves POST ...:exportAssets for project parents.
// Wired from Cloud Resource Manager's v1 project POST dispatcher to avoid ServeMux conflict.
func HandleProjectExport(w http.ResponseWriter, r *http.Request, st *store.Store, eval *authz.Evaluator, projectID string, p authn.Principal) {
	svc := &Service{Store: st, Authz: eval}
	svc.exportAssets(w, r, p, "projects/"+projectID, projectID)
}

func splitColon(seg string) (id, action string) {
	if i := strings.IndexByte(seg, ':'); i >= 0 {
		return seg[:i], seg[i+1:]
	}
	return seg, ""
}

func (s *Service) projectGETColon(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	raw := r.PathValue("project")
	projectID, action := splitColon(raw)
	if projectID == "" || action == "" {
		gcperrors.InvalidArgument(w, "expected projects/{project}:searchAllResources or :batchGetAssetsHistory")
		return
	}
	parent := "projects/" + projectID
	switch action {
	case "searchAllResources":
		s.searchAllResources(w, r, p, parent, projectID)
	case "batchGetAssetsHistory":
		s.batchGetAssetsHistory(w, r, p, parent, projectID)
	default:
		gcperrors.InvalidArgument(w, "unknown cloudasset v1 project method")
	}
}

func (s *Service) folderGETColon(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	raw := r.PathValue("folder")
	folderID, action := splitColon(raw)
	if folderID == "" || action == "" {
		gcperrors.InvalidArgument(w, "expected folders/{folder}:searchAllResources")
		return
	}
	parent := "folders/" + folderID
	switch action {
	case "searchAllResources":
		// Lab: folder scope searches the default project inventory.
		s.searchAllResources(w, r, p, parent, "")
	case "batchGetAssetsHistory":
		s.batchGetAssetsHistory(w, r, p, parent, "")
	default:
		gcperrors.InvalidArgument(w, "unknown cloudasset v1 folder method")
	}
}

func (s *Service) orgGETColon(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	raw := r.PathValue("organization")
	orgID, action := splitColon(raw)
	if orgID == "" || action == "" {
		gcperrors.InvalidArgument(w, "expected organizations/{organization}:searchAllResources")
		return
	}
	parent := "organizations/" + orgID
	switch action {
	case "searchAllResources":
		s.searchAllResources(w, r, p, parent, "")
	case "batchGetAssetsHistory":
		s.batchGetAssetsHistory(w, r, p, parent, "")
	default:
		gcperrors.InvalidArgument(w, "unknown cloudasset v1 organization method")
	}
}

func (s *Service) folderPOSTColon(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	raw := r.PathValue("folder")
	folderID, action := splitColon(raw)
	if folderID == "" || action != "exportAssets" {
		gcperrors.InvalidArgument(w, "expected folders/{folder}:exportAssets")
		return
	}
	s.exportAssets(w, r, p, "folders/"+folderID, "")
}

func (s *Service) orgPOSTColon(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	raw := r.PathValue("organization")
	orgID, action := splitColon(raw)
	if orgID == "" || action != "exportAssets" {
		gcperrors.InvalidArgument(w, "expected organizations/{organization}:exportAssets")
		return
	}
	s.exportAssets(w, r, p, "organizations/"+orgID, "")
}

func (s *Service) listAssets(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	projectID := r.PathValue("project")
	s.listAssetsForParent(w, r, p, "projects/"+projectID, projectID)
}

func (s *Service) listAssetsFolder(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	folderID := r.PathValue("folder")
	s.listAssetsForParent(w, r, p, "folders/"+folderID, "")
}

func (s *Service) listAssetsOrg(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	orgID := r.PathValue("organization")
	s.listAssetsForParent(w, r, p, "organizations/"+orgID, "")
}

func (s *Service) requireScope(p authn.Principal, permission, parent, projectID string) error {
	resource := parent
	if projectID != "" {
		resource = "projects/" + projectID
	}
	return restlab.Evaluate(s.Authz, p, permission, resource)
}

func (s *Service) inventoryForScope(projectID string) ([]store.InventoryAsset, error) {
	if projectID != "" {
		return s.Store.ListInventoryAssets(projectID)
	}
	projects, err := s.Store.ListProjects()
	if err != nil {
		return nil, err
	}
	var out []store.InventoryAsset
	for _, p := range projects {
		part, err := s.Store.ListInventoryAssets(p.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, part...)
	}
	return out, nil
}

func (s *Service) searchAllResources(w http.ResponseWriter, r *http.Request, p authn.Principal, parent, projectID string) {
	if err := s.requireScope(p, "cloudasset.assets.searchAllResources", parent, projectID); err != nil {
		restlab.WriteAuthzErr(w, err)
		return
	}
	assets, err := s.inventoryForScope(projectID)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	q := r.URL.Query()
	query := strings.TrimSpace(q.Get("query"))
	assetTypes := q["assetTypes"]
	if len(assetTypes) == 1 && strings.Contains(assetTypes[0], ",") {
		assetTypes = splitCSV(assetTypes[0])
	}
	filtered := filterAssets(assets, query, assetTypes)
	pageSize := pageSizeOf(q.Get("pageSize"), 100, 500)
	offset := pageOffset(q.Get("pageToken"))
	if offset > len(filtered) {
		offset = len(filtered)
	}
	end := offset + pageSize
	if end > len(filtered) {
		end = len(filtered)
	}
	page := filtered[offset:end]
	results := make([]map[string]any, 0, len(page))
	for _, a := range page {
		results = append(results, searchResultJSON(a))
	}
	resp := map[string]any{"results": results}
	if end < len(filtered) {
		resp["nextPageToken"] = strconv.Itoa(end)
	}
	restlab.WriteJSON(w, http.StatusOK, resp)
}

func (s *Service) listAssetsForParent(w http.ResponseWriter, r *http.Request, p authn.Principal, parent, projectID string) {
	if err := s.requireScope(p, "cloudasset.assets.listResource", parent, projectID); err != nil {
		restlab.WriteAuthzErr(w, err)
		return
	}
	assets, err := s.inventoryForScope(projectID)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	q := r.URL.Query()
	assetTypes := q["assetTypes"]
	if len(assetTypes) == 1 && strings.Contains(assetTypes[0], ",") {
		assetTypes = splitCSV(assetTypes[0])
	}
	filtered := filterAssets(assets, "", assetTypes)
	pageSize := pageSizeOf(q.Get("pageSize"), 100, 1000)
	offset := pageOffset(q.Get("pageToken"))
	if offset > len(filtered) {
		offset = len(filtered)
	}
	end := offset + pageSize
	if end > len(filtered) {
		end = len(filtered)
	}
	page := filtered[offset:end]
	out := make([]map[string]any, 0, len(page))
	for _, a := range page {
		out = append(out, assetJSON(a))
	}
	resp := map[string]any{"assets": out}
	if end < len(filtered) {
		resp["nextPageToken"] = strconv.Itoa(end)
	}
	restlab.WriteJSON(w, http.StatusOK, resp)
}

func (s *Service) exportAssets(w http.ResponseWriter, r *http.Request, p authn.Principal, parent, projectID string) {
	if err := s.requireScope(p, "cloudasset.assets.exportResource", parent, projectID); err != nil {
		restlab.WriteAuthzErr(w, err)
		return
	}
	body, err := readJSONObject(r)
	if err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	outputConfig, _ := body["outputConfig"].(map[string]any)
	if outputConfig == nil {
		gcperrors.InvalidArgument(w, "outputConfig is required")
		return
	}
	assetTypes, _ := stringSlice(body["assetTypes"])
	assets, err := s.inventoryForScope(projectID)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	filtered := filterAssets(assets, "", assetTypes)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	opID := "operation-" + uuid.NewString()
	opName := parent + "/operations/" + opID

	// Theatre: record a history snapshot per exported asset for batchGetAssetsHistory labs.
	for _, a := range filtered {
		_ = s.Store.InsertCloudAssetHistory(store.CloudAssetHistoryRow{
			Parent:      parent,
			AssetName:   a.Name,
			AssetType:   a.AssetType,
			ContentJSON: a.DataJSON,
			WindowStart: now,
			WindowEnd:   now,
			CreatedAt:   now,
		})
	}

	uris := []string{}
	if gcs, ok := outputConfig["gcsDestination"].(map[string]any); ok {
		if uri, _ := gcs["uri"].(string); uri != "" {
			uris = append(uris, uri)
		}
		if prefix, _ := gcs["uriPrefix"].(string); prefix != "" {
			uris = append(uris, strings.TrimRight(prefix, "/")+"/assets.json")
		}
	}
	if len(uris) == 0 {
		uris = append(uris, "gs://noctaxris-gcp-lab/cloudasset/"+opID+"/assets.json")
	}

	restlab.WriteJSON(w, http.StatusOK, map[string]any{
		"name": opName,
		"done": true,
		"response": map[string]any{
			"@type":        "type.googleapis.com/google.cloud.asset.v1.ExportAssetsResponse",
			"readTime":     now,
			"outputConfig": outputConfig,
			"outputResult": map[string]any{
				"gcsResult": map[string]any{"uris": uris},
			},
		},
		"metadata": map[string]any{
			"@type":      "type.googleapis.com/google.cloud.asset.v1.ExportAssetsRequest",
			"parent":     parent,
			"assetTypes": assetTypes,
			"assetCount": len(filtered),
		},
	})
}

func (s *Service) batchGetAssetsHistory(w http.ResponseWriter, r *http.Request, p authn.Principal, parent, projectID string) {
	if err := s.requireScope(p, "cloudasset.assets.exportResource", parent, projectID); err != nil {
		restlab.WriteAuthzErr(w, err)
		return
	}
	q := r.URL.Query()
	assetNames := q["assetNames"]
	if len(assetNames) == 1 && strings.Contains(assetNames[0], ",") {
		assetNames = splitCSV(assetNames[0])
	}
	rows, err := s.Store.ListCloudAssetHistory(parent, assetNames)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	assets := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		var data any
		_ = json.Unmarshal([]byte(row.ContentJSON), &data)
		assets = append(assets, map[string]any{
			"window": map[string]any{
				"startTime": row.WindowStart,
				"endTime":   row.WindowEnd,
			},
			"deleted": false,
			"asset": map[string]any{
				"name":      row.AssetName,
				"assetType": row.AssetType,
				"resource": map[string]any{
					"version": "v1",
					"data":    data,
				},
			},
		})
	}
	restlab.WriteJSON(w, http.StatusOK, map[string]any{"assets": assets})
}

func (s *Service) createFeed(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	projectID := r.PathValue("project")
	parent := "projects/" + projectID
	if err := restlab.Require(s.Authz, p, "cloudasset.feeds.create", projectID); err != nil {
		restlab.WriteAuthzErr(w, err)
		return
	}
	feedID := strings.TrimSpace(r.URL.Query().Get("feedId"))
	body, err := readJSONObject(r)
	if err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	feedObj, _ := body["feed"].(map[string]any)
	if feedObj == nil {
		feedObj = body
	}
	if feedID == "" {
		if id, _ := feedObj["name"].(string); id != "" {
			parts := strings.Split(id, "/")
			feedID = parts[len(parts)-1]
		}
	}
	if feedID == "" {
		gcperrors.InvalidArgument(w, "feedId is required")
		return
	}
	assetTypes, _ := stringSlice(feedObj["assetTypes"])
	typesJSON, _ := json.Marshal(assetTypes)
	contentType, _ := feedObj["contentType"].(string)
	pubsubTopic := ""
	if out, ok := feedObj["feedOutputConfig"].(map[string]any); ok {
		if pub, ok := out["pubsubDestination"].(map[string]any); ok {
			pubsubTopic, _ = pub["topic"].(string)
		}
	}
	bodyJSON, _ := json.Marshal(feedObj)
	created, ok, err := s.Store.CreateCloudAssetFeed(store.CloudAssetFeed{
		Parent:         parent,
		FeedID:         feedID,
		AssetTypesJSON: string(typesJSON),
		ContentType:    contentType,
		PubsubTopic:    pubsubTopic,
		BodyJSON:       string(bodyJSON),
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "Feed already exists.")
		return
	}
	restlab.WriteJSON(w, http.StatusOK, feedJSON(created))
}

func (s *Service) listFeeds(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	projectID := r.PathValue("project")
	if err := restlab.Require(s.Authz, p, "cloudasset.feeds.list", projectID); err != nil {
		restlab.WriteAuthzErr(w, err)
		return
	}
	feeds, err := s.Store.ListCloudAssetFeeds("projects/" + projectID)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(feeds))
	for _, f := range feeds {
		out = append(out, feedJSON(f))
	}
	restlab.WriteJSON(w, http.StatusOK, map[string]any{"feeds": out})
}

func (s *Service) getFeed(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	projectID := r.PathValue("project")
	feedID := r.PathValue("feed")
	if err := restlab.Require(s.Authz, p, "cloudasset.feeds.get", projectID); err != nil {
		restlab.WriteAuthzErr(w, err)
		return
	}
	name := "projects/" + projectID + "/feeds/" + feedID
	f, ok, err := s.Store.GetCloudAssetFeed(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Requested entity was not found.")
		return
	}
	restlab.WriteJSON(w, http.StatusOK, feedJSON(f))
}

func (s *Service) deleteFeed(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	projectID := r.PathValue("project")
	feedID := r.PathValue("feed")
	if err := restlab.Require(s.Authz, p, "cloudasset.feeds.delete", projectID); err != nil {
		restlab.WriteAuthzErr(w, err)
		return
	}
	name := "projects/" + projectID + "/feeds/" + feedID
	ok, err := s.Store.DeleteCloudAssetFeed(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Requested entity was not found.")
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("{}"))
}

func feedJSON(f store.CloudAssetFeed) map[string]any {
	var assetTypes []string
	_ = json.Unmarshal([]byte(f.AssetTypesJSON), &assetTypes)
	if assetTypes == nil {
		assetTypes = []string{}
	}
	out := map[string]any{
		"name":        f.Name,
		"assetTypes":  assetTypes,
		"contentType": f.ContentType,
	}
	if f.PubsubTopic != "" {
		out["feedOutputConfig"] = map[string]any{
			"pubsubDestination": map[string]any{"topic": f.PubsubTopic},
		}
	}
	return out
}

func searchResultJSON(a store.InventoryAsset) map[string]any {
	labels := map[string]string{}
	_ = json.Unmarshal([]byte(a.LabelsJSON), &labels)
	return map[string]any{
		"name":        a.Name,
		"assetType":   a.AssetType,
		"project":     "projects/" + a.ProjectID,
		"displayName": a.DisplayName,
		"location":    a.Location,
		"labels":      labels,
		"createTime":  a.CreateTime,
		"updateTime":  a.UpdateTime,
		"state":       a.State,
	}
}

func assetJSON(a store.InventoryAsset) map[string]any {
	var data any
	_ = json.Unmarshal([]byte(a.DataJSON), &data)
	return map[string]any{
		"name":      a.Name,
		"assetType": a.AssetType,
		"resource": map[string]any{
			"version":          "v1",
			"discoveryName":    discoveryName(a.AssetType),
			"parent":           "//cloudresourcemanager.googleapis.com/projects/" + a.ProjectID,
			"data":             data,
			"location":         a.Location,
		},
	}
}

func discoveryName(assetType string) string {
	if i := strings.LastIndex(assetType, "/"); i >= 0 {
		return assetType[i+1:]
	}
	return assetType
}

func filterAssets(assets []store.InventoryAsset, query string, assetTypes []string) []store.InventoryAsset {
	var typeMatchers []*regexp.Regexp
	for _, t := range assetTypes {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		re, err := regexp.Compile("(?i)^" + t + "$")
		if err != nil {
			continue
		}
		typeMatchers = append(typeMatchers, re)
	}
	query = strings.TrimSpace(query)
	queryLower := strings.ToLower(query)
	nameField := ""
	if strings.HasPrefix(queryLower, "name:") {
		nameField = strings.TrimSpace(query[len("name:"):])
		nameField = strings.Trim(nameField, `"`)
	} else if strings.HasPrefix(queryLower, "name=") {
		nameField = strings.TrimSpace(query[len("name="):])
		nameField = strings.Trim(nameField, `"`)
	}
	var out []store.InventoryAsset
	for _, a := range assets {
		if len(typeMatchers) > 0 {
			matched := false
			for _, re := range typeMatchers {
				if re.MatchString(a.AssetType) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		if query == "" {
			out = append(out, a)
			continue
		}
		if nameField != "" {
			if strings.Contains(strings.ToLower(a.Name), strings.ToLower(nameField)) ||
				strings.Contains(strings.ToLower(a.DisplayName), strings.ToLower(nameField)) {
				out = append(out, a)
			}
			continue
		}
		hay := strings.ToLower(a.Name + " " + a.DisplayName + " " + a.AssetType + " " + a.Location + " " + a.State)
		if strings.Contains(hay, queryLower) {
			out = append(out, a)
		}
	}
	return out
}

func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func pageSizeOf(raw string, def, max int) int {
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

func pageOffset(token string) int {
	if token == "" {
		return 0
	}
	n, err := strconv.Atoi(token)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func readJSONObject(r *http.Request) (map[string]any, error) {
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

func stringSlice(v any) ([]string, bool) {
	switch t := v.(type) {
	case []string:
		return t, true
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			if s, ok := x.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out, true
	default:
		return nil, false
	}
}
