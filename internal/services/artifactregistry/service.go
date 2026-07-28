package artifactregistry

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

// DefaultLocation is the lab default Artifact Registry location.
const DefaultLocation = "us-central1"

// Service serves Artifact Registry REST v1 (repos / packages / versions metadata).
type Service struct {
	Store *store.Store
	Authz *authz.Evaluator
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// Mount registers Artifact Registry v1 REST routes.
// Colon methods are parsed from wildcard path segments via splitColonAction.
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/repositories", s.wrap(principalFrom, s.listRepositories))
	mux.HandleFunc("POST /v1/projects/{project}/locations/{location}/repositories", s.wrap(principalFrom, s.createRepository))
	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/repositories/{repository}", s.wrap(principalFrom, s.getRepository))
	mux.HandleFunc("POST /v1/projects/{project}/locations/{location}/repositories/{repository}", s.wrap(principalFrom, s.repositoryPOSTAction))
	mux.HandleFunc("PATCH /v1/projects/{project}/locations/{location}/repositories/{repository}", s.wrap(principalFrom, s.patchRepository))
	mux.HandleFunc("DELETE /v1/projects/{project}/locations/{location}/repositories/{repository}", s.wrap(principalFrom, s.deleteRepository))

	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/repositories/{repository}/files", s.wrap(principalFrom, s.listFiles))

	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/repositories/{repository}/packages", s.wrap(principalFrom, s.listPackages))
	mux.HandleFunc("POST /v1/projects/{project}/locations/{location}/repositories/{repository}/packages", s.wrap(principalFrom, s.createPackage))
	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/repositories/{repository}/packages/{package}", s.wrap(principalFrom, s.getPackage))
	mux.HandleFunc("DELETE /v1/projects/{project}/locations/{location}/repositories/{repository}/packages/{package}", s.wrap(principalFrom, s.deletePackage))

	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/repositories/{repository}/packages/{package}/tags", s.wrap(principalFrom, s.listTags))

	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/repositories/{repository}/packages/{package}/versions", s.wrap(principalFrom, s.listVersions))
	mux.HandleFunc("POST /v1/projects/{project}/locations/{location}/repositories/{repository}/packages/{package}/versions", s.wrap(principalFrom, s.createVersion))
	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/repositories/{repository}/packages/{package}/versions/{version}", s.wrap(principalFrom, s.getVersion))
	mux.HandleFunc("DELETE /v1/projects/{project}/locations/{location}/repositories/{repository}/packages/{package}/versions/{version}", s.wrap(principalFrom, s.deleteVersion))
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

func splitColonAction(seg string) (name, action string) {
	if i := strings.IndexByte(seg, ':'); i >= 0 {
		return seg[:i], seg[i+1:]
	}
	return seg, ""
}

func repoName(project, location, id string) string {
	return fmt.Sprintf("projects/%s/locations/%s/repositories/%s", project, location, id)
}

func packageName(repo, pkgID string) string {
	return repo + "/packages/" + pkgID
}

func versionName(pkg, verID string) string {
	return pkg + "/versions/" + verID
}

func (s *Service) createRepository(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	if err := s.require(p, "artifactregistry.repositories.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	repoID := r.URL.Query().Get("repositoryId")
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body == nil {
		body = map[string]any{}
	}
	if repoID == "" {
		if n, _ := body["name"].(string); n != "" {
			parts := strings.Split(n, "/")
			repoID = parts[len(parts)-1]
		}
	}
	if repoID == "" {
		gcperrors.InvalidArgument(w, "repositoryId is required")
		return
	}
	format, _ := body["format"].(string)
	if format == "" {
		format = "DOCKER"
	}
	format = strings.ToUpper(format)
	desc, _ := body["description"].(string)
	mode, _ := body["mode"].(string)
	labelsJSON := "{}"
	if labels, ok := body["labels"]; ok {
		raw, _ := json.Marshal(labels)
		labelsJSON = string(raw)
	}
	name := repoName(project, location, repoID)
	created, err := s.Store.CreateArRepository(store.ArRepository{
		Name: name, ProjectID: project, Location: location, RepositoryID: repoID,
		Format: format, Description: desc, LabelsJSON: labelsJSON, Mode: mode,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "repository already exists")
		return
	}
	out, _, _ := s.Store.GetArRepository(name)
	writeJSON(w, http.StatusOK, toRepoJSON(out))
}

func (s *Service) listRepositories(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	if err := s.require(p, "artifactregistry.repositories.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	list, err := s.Store.ListArRepositories(project, location)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, repo := range list {
		items = append(items, toRepoJSON(repo))
	}
	writeJSON(w, http.StatusOK, map[string]any{"repositories": items})
}

func (s *Service) getRepository(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	id, action := splitColonAction(r.PathValue("repository"))
	switch action {
	case "getIamPolicy":
		s.getIamPolicy(w, r, p, project, location, id)
		return
	case "":
		// get repository
	default:
		gcperrors.NotFound(w, "unknown Artifact Registry method")
		return
	}
	if err := s.require(p, "artifactregistry.repositories.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	repo, ok, err := s.Store.GetArRepository(repoName(project, location, id))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Repository not found")
		return
	}
	writeJSON(w, http.StatusOK, toRepoJSON(repo))
}

func (s *Service) repositoryPOSTAction(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	id, action := splitColonAction(r.PathValue("repository"))
	switch action {
	case "setIamPolicy":
		s.setIamPolicy(w, r, p, project, location, id)
	case "getIamPolicy":
		// Some clients POST getIamPolicy; accept both.
		s.getIamPolicy(w, r, p, project, location, id)
	default:
		gcperrors.NotFound(w, "unknown Artifact Registry method")
	}
}

func (s *Service) getIamPolicy(w http.ResponseWriter, _ *http.Request, p authn.Principal, project, location, id string) {
	if err := s.require(p, "artifactregistry.repositories.getIamPolicy", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := repoName(project, location, id)
	if _, ok, err := s.Store.GetArRepository(name); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "Repository not found")
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
	if err := s.require(p, "artifactregistry.repositories.setIamPolicy", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := repoName(project, location, id)
	if _, ok, err := s.Store.GetArRepository(name); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "Repository not found")
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

func (s *Service) patchRepository(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	id, _ := splitColonAction(r.PathValue("repository"))
	if err := s.require(p, "artifactregistry.repositories.update", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body == nil {
		body = map[string]any{}
	}
	mask := r.URL.Query().Get("updateMask")
	wantDesc := mask == "" || fieldMaskHas(mask, "description")
	wantLabels := mask == "" || fieldMaskHas(mask, "labels")
	var descPtr *string
	var labelsPtr *string
	if wantDesc {
		if desc, ok := body["description"].(string); ok {
			descPtr = &desc
		} else if mask != "" {
			empty := ""
			descPtr = &empty
		}
	}
	if wantLabels {
		if labels, ok := body["labels"]; ok {
			raw, _ := json.Marshal(labels)
			s := string(raw)
			labelsPtr = &s
		} else if mask != "" {
			empty := "{}"
			labelsPtr = &empty
		}
	}
	repo, ok, err := s.Store.PatchArRepositoryDeepen(repoName(project, location, id), descPtr, labelsPtr)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Repository not found")
		return
	}
	writeJSON(w, http.StatusOK, toRepoJSON(repo))
}

func fieldMaskHas(mask, field string) bool {
	for _, p := range strings.Split(mask, ",") {
		if strings.TrimSpace(p) == field {
			return true
		}
	}
	return false
}

func (s *Service) listFiles(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	repoID, _ := splitColonAction(r.PathValue("repository"))
	if err := s.require(p, "artifactregistry.files.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := repoName(project, location, repoID)
	if _, ok, err := s.Store.GetArRepository(name); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "Repository not found")
		return
	}
	list, err := s.Store.ListArFilesTheatreDeepen(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, f := range list {
		items = append(items, map[string]any{
			"name":       f.Name,
			"sizeBytes":  f.SizeBytes,
			"owner":      f.Owner,
			"createTime": f.CreateTime,
			"updateTime": f.UpdateTime,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": items})
}

func (s *Service) listTags(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	repoID, _ := splitColonAction(r.PathValue("repository"))
	pkgID, _ := splitColonAction(r.PathValue("package"))
	if err := s.require(p, "artifactregistry.tags.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	pkgName := packageName(repoName(project, location, repoID), pkgID)
	if _, ok, err := s.Store.GetArPackage(pkgName); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "Package not found")
		return
	}
	list, err := s.Store.ListArTagsTheatreDeepen(pkgName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, t := range list {
		items = append(items, map[string]any{
			"name":    t.Name,
			"version": t.Version,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": items})
}

func (s *Service) deleteRepository(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	id := r.PathValue("repository")
	if err := s.require(p, "artifactregistry.repositories.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	ok, err := s.Store.DeleteArRepository(repoName(project, location, id))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Repository not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Service) createPackage(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	repoID := r.PathValue("repository")
	if err := s.require(p, "artifactregistry.packages.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	repo := repoName(project, location, repoID)
	if _, ok, err := s.Store.GetArRepository(repo); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "Repository not found")
		return
	}
	pkgID := r.URL.Query().Get("packageId")
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body == nil {
		body = map[string]any{}
	}
	if pkgID == "" {
		if n, _ := body["name"].(string); n != "" {
			parts := strings.Split(n, "/")
			pkgID = parts[len(parts)-1]
		}
	}
	if pkgID == "" {
		if id, _ := body["packageId"].(string); id != "" {
			pkgID = id
		}
	}
	if pkgID == "" {
		gcperrors.InvalidArgument(w, "packageId is required")
		return
	}
	display, _ := body["displayName"].(string)
	name := packageName(repo, pkgID)
	created, err := s.Store.CreateArPackage(store.ArPackage{
		Name: name, RepositoryName: repo, PackageID: pkgID, DisplayName: display,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "package already exists")
		return
	}
	out, _, _ := s.Store.GetArPackage(name)
	writeJSON(w, http.StatusOK, toPackageJSON(out))
}

func (s *Service) listPackages(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	repoID := r.PathValue("repository")
	if err := s.require(p, "artifactregistry.packages.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	list, err := s.Store.ListArPackages(repoName(project, location, repoID))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, pkg := range list {
		items = append(items, toPackageJSON(pkg))
	}
	writeJSON(w, http.StatusOK, map[string]any{"packages": items})
}

func (s *Service) getPackage(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	repoID := r.PathValue("repository")
	pkgID := r.PathValue("package")
	if err := s.require(p, "artifactregistry.packages.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	pkg, ok, err := s.Store.GetArPackage(packageName(repoName(project, location, repoID), pkgID))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Package not found")
		return
	}
	writeJSON(w, http.StatusOK, toPackageJSON(pkg))
}

func (s *Service) deletePackage(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	repoID := r.PathValue("repository")
	pkgID := r.PathValue("package")
	if err := s.require(p, "artifactregistry.packages.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	ok, err := s.Store.DeleteArPackage(packageName(repoName(project, location, repoID), pkgID))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Package not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Service) createVersion(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	repoID := r.PathValue("repository")
	pkgID := r.PathValue("package")
	if err := s.require(p, "artifactregistry.versions.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	pkgName := packageName(repoName(project, location, repoID), pkgID)
	if _, ok, err := s.Store.GetArPackage(pkgName); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "Package not found")
		return
	}
	verID := r.URL.Query().Get("versionId")
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body == nil {
		body = map[string]any{}
	}
	if verID == "" {
		if n, _ := body["name"].(string); n != "" {
			parts := strings.Split(n, "/")
			verID = parts[len(parts)-1]
		}
	}
	if verID == "" {
		if id, _ := body["versionId"].(string); id != "" {
			verID = id
		}
	}
	if verID == "" {
		gcperrors.InvalidArgument(w, "versionId is required")
		return
	}
	desc, _ := body["description"].(string)
	tagsJSON := "[]"
	if tags, ok := body["relatedTags"]; ok {
		raw, _ := json.Marshal(tags)
		tagsJSON = string(raw)
	}
	metaJSON := "{}"
	if meta, ok := body["metadata"]; ok {
		raw, _ := json.Marshal(meta)
		metaJSON = string(raw)
	}
	name := versionName(pkgName, verID)
	created, err := s.Store.CreateArVersion(store.ArVersion{
		Name: name, PackageName: pkgName, VersionID: verID, Description: desc,
		RelatedTagsJSON: tagsJSON, MetadataJSON: metaJSON,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "version already exists")
		return
	}
	out, _, _ := s.Store.GetArVersion(name)
	writeJSON(w, http.StatusOK, toVersionJSON(out))
}

func (s *Service) listVersions(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	repoID := r.PathValue("repository")
	pkgID := r.PathValue("package")
	if err := s.require(p, "artifactregistry.versions.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	list, err := s.Store.ListArVersions(packageName(repoName(project, location, repoID), pkgID))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, v := range list {
		items = append(items, toVersionJSON(v))
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": items})
}

func (s *Service) getVersion(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	repoID := r.PathValue("repository")
	pkgID := r.PathValue("package")
	verID := r.PathValue("version")
	if err := s.require(p, "artifactregistry.versions.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	v, ok, err := s.Store.GetArVersion(versionName(packageName(repoName(project, location, repoID), pkgID), verID))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Version not found")
		return
	}
	writeJSON(w, http.StatusOK, toVersionJSON(v))
}

func (s *Service) deleteVersion(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	repoID := r.PathValue("repository")
	pkgID := r.PathValue("package")
	verID := r.PathValue("version")
	if err := s.require(p, "artifactregistry.versions.delete", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	ok, err := s.Store.DeleteArVersion(versionName(packageName(repoName(project, location, repoID), pkgID), verID))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Version not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func toRepoJSON(repo store.ArRepository) map[string]any {
	var labels any
	_ = json.Unmarshal([]byte(repo.LabelsJSON), &labels)
	if labels == nil {
		labels = map[string]any{}
	}
	uri := fmt.Sprintf("%s-docker.pkg.dev/%s/%s", repo.Location, repo.ProjectID, repo.RepositoryID)
	if !strings.EqualFold(repo.Format, "DOCKER") {
		uri = fmt.Sprintf("%s-pkg.dev/%s/%s", repo.Location, repo.ProjectID, repo.RepositoryID)
	}
	return map[string]any{
		"name":        repo.Name,
		"format":      repo.Format,
		"description": repo.Description,
		"labels":      labels,
		"createTime":  repo.CreatedAt,
		"updateTime":  repo.UpdatedAt,
		"mode":        repo.Mode,
		"sizeBytes":   "0",
		"registryUri": uri,
	}
}

func toPackageJSON(pkg store.ArPackage) map[string]any {
	return map[string]any{
		"name":        pkg.Name,
		"displayName": pkg.DisplayName,
		"createTime":  pkg.CreatedAt,
		"updateTime":  pkg.UpdatedAt,
	}
}

func toVersionJSON(v store.ArVersion) map[string]any {
	var tags any
	_ = json.Unmarshal([]byte(v.RelatedTagsJSON), &tags)
	if tags == nil {
		tags = []any{}
	}
	var meta any
	_ = json.Unmarshal([]byte(v.MetadataJSON), &meta)
	if meta == nil {
		meta = map[string]any{}
	}
	return map[string]any{
		"name":        v.Name,
		"description": v.Description,
		"createTime":  v.CreatedAt,
		"updateTime":  v.UpdatedAt,
		"relatedTags": tags,
		"metadata":    meta,
	}
}
