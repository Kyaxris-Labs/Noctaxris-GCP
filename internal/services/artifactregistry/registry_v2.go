package artifactregistry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/config"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
	"github.com/google/uuid"
)

const (
	registryAPIVersion    = "registry/2.0"
	registryVersionHeader = "Docker-Distribution-API-Version"
	maxRegistryBlobBytes  = 32 << 20
	defaultManifestType   = "application/vnd.docker.distribution.manifest.v2+json"
)

type registryUpload struct {
	name string
}

type registryV2Op struct {
	name string
	kind string // uploads-start, uploads-put, blob, manifest
	id   string
}

var (
	registryPullPerms = []string{
		"artifactregistry.repositories.downloadArtifacts",
		"artifactregistry.dockerimages.get",
	}
	registryPushPerms = []string{
		"artifactregistry.repositories.uploadArtifacts",
		"artifactregistry.dockerimages.create",
	}
)

// IsRegistryV2Path reports Docker Registry HTTP API V2 routes on the shared listener
// (not Cloud Logging /v2/entries or /v2/projects/...).
func IsRegistryV2Path(path string) bool {
	if path == "/v2" || path == "/v2/" {
		return true
	}
	if !strings.HasPrefix(path, "/v2/") {
		return false
	}
	return strings.Contains(path, "/blobs/") || strings.Contains(path, "/manifests/")
}

func (s *Service) mountRegistryV2(mux *http.ServeMux, principalFrom principalFunc) {
	s.uploadsMu.Lock()
	if s.uploads == nil {
		s.uploads = make(map[string]*registryUpload)
	}
	s.uploadsMu.Unlock()

	mux.HandleFunc("GET /v2", s.wrapRegistry(principalFrom, s.registryV2Ping))
	mux.HandleFunc("GET /v2/{name...}", s.wrapRegistry(principalFrom, s.registryV2Get))
	mux.HandleFunc("POST /v2/{name...}", s.wrapRegistry(principalFrom, s.registryV2Post))
	mux.HandleFunc("PUT /v2/{name...}", s.wrapRegistry(principalFrom, s.registryV2Put))
}

func (s *Service) wrapRegistry(principalFrom principalFunc, h handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(registryVersionHeader, registryAPIVersion)
		p, ok := principalFrom(r)
		if !ok {
			writeRegistryUnauthorized(w)
			return
		}
		h(w, r, p)
	}
}

func (s *Service) registryV2Ping(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	if err := s.requireRegistry(p, "", false); err != nil {
		writeRegistryAuthz(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write([]byte("{}\n"))
}

func (s *Service) registryV2Get(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	s.registryV2Read(w, r, p, r.Method == http.MethodHead)
}

func (s *Service) registryV2Read(w http.ResponseWriter, r *http.Request, p authn.Principal, head bool) {
	rest := r.PathValue("name")
	if strings.Trim(rest, "/") == "" {
		s.registryV2Ping(w, r, p)
		return
	}
	op, ok := parseRegistryV2Rest(rest)
	if !ok {
		writeRegistryError(w, http.StatusNotFound, "NAME_UNKNOWN", "repository name not known to registry")
		return
	}
	switch op.kind {
	case "blob":
		s.registryGetBlob(w, p, op.name, op.id, head)
	case "manifest":
		s.registryGetManifest(w, p, op.name, op.id, head)
	default:
		writeRegistryError(w, http.StatusNotFound, "UNSUPPORTED", "unknown registry method")
	}
}

func (s *Service) registryV2Post(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	op, ok := parseRegistryV2Rest(r.PathValue("name"))
	if !ok || op.kind != "uploads-start" || op.name == "" {
		writeRegistryError(w, http.StatusNotFound, "NAME_UNKNOWN", "repository name not known to registry")
		return
	}
	if err := s.requireRegistry(p, op.name, true); err != nil {
		writeRegistryAuthz(w, err)
		return
	}
	id := uuid.NewString()
	s.uploadsMu.Lock()
	s.uploads[id] = &registryUpload{name: op.name}
	s.uploadsMu.Unlock()
	loc := "/v2/" + op.name + "/blobs/uploads/" + id
	w.Header().Set("Location", loc)
	w.Header().Set("Docker-Upload-UUID", id)
	w.Header().Set("Range", "0-0")
	w.WriteHeader(http.StatusAccepted)
}

func (s *Service) registryV2Put(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	op, ok := parseRegistryV2Rest(r.PathValue("name"))
	if !ok {
		writeRegistryError(w, http.StatusNotFound, "NAME_UNKNOWN", "repository name not known to registry")
		return
	}
	switch op.kind {
	case "uploads-put":
		s.registryPutBlob(w, r, p, op.name, op.id)
	case "manifest":
		s.registryPutManifest(w, r, p, op.name, op.id)
	default:
		writeRegistryError(w, http.StatusNotFound, "UNSUPPORTED", "unknown registry method")
	}
}

func (s *Service) registryGetBlob(w http.ResponseWriter, p authn.Principal, name, digest string, head bool) {
	if err := s.requireRegistry(p, name, false); err != nil {
		writeRegistryAuthz(w, err)
		return
	}
	digest = normalizeDigest(digest)
	data, ok, err := s.Store.GetArRegistryBlob(digest)
	if err != nil {
		writeRegistryError(w, http.StatusInternalServerError, "UNKNOWN", err.Error())
		return
	}
	if !ok {
		writeRegistryError(w, http.StatusNotFound, "BLOB_UNKNOWN", "blob unknown to registry")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Docker-Content-Digest", digest)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	w.WriteHeader(http.StatusOK)
	if head {
		return
	}
	_, _ = w.Write(data)
}

func (s *Service) registryGetManifest(w http.ResponseWriter, p authn.Principal, name, reference string, head bool) {
	if err := s.requireRegistry(p, name, false); err != nil {
		writeRegistryAuthz(w, err)
		return
	}
	m, ok, err := s.Store.GetArRegistryManifest(name, reference)
	if err != nil {
		writeRegistryError(w, http.StatusInternalServerError, "UNKNOWN", err.Error())
		return
	}
	if !ok {
		writeRegistryError(w, http.StatusNotFound, "MANIFEST_UNKNOWN", "manifest unknown")
		return
	}
	w.Header().Set("Content-Type", m.MediaType)
	w.Header().Set("Docker-Content-Digest", m.Digest)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(m.Data)))
	w.WriteHeader(http.StatusOK)
	if head {
		return
	}
	_, _ = w.Write(m.Data)
}

func (s *Service) registryPutBlob(w http.ResponseWriter, r *http.Request, p authn.Principal, name, uploadID string) {
	if err := s.requireRegistry(p, name, true); err != nil {
		writeRegistryAuthz(w, err)
		return
	}
	s.uploadsMu.Lock()
	sess, ok := s.uploads[uploadID]
	s.uploadsMu.Unlock()
	if !ok || sess == nil {
		writeRegistryError(w, http.StatusNotFound, "BLOB_UPLOAD_UNKNOWN", "blob upload unknown to registry")
		return
	}
	if sess.name != name {
		writeRegistryError(w, http.StatusBadRequest, "NAME_INVALID", "upload uuid does not match repository")
		return
	}
	digest := normalizeDigest(r.URL.Query().Get("digest"))
	if !validDigest(digest) {
		writeRegistryError(w, http.StatusBadRequest, "DIGEST_INVALID", "digest required")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRegistryBlobBytes+1))
	if err != nil {
		writeRegistryError(w, http.StatusBadRequest, "BLOB_UPLOAD_INVALID", "read blob")
		return
	}
	if len(body) > maxRegistryBlobBytes {
		writeRegistryError(w, http.StatusRequestEntityTooLarge, "BLOB_UPLOAD_INVALID", "blob too large")
		return
	}
	got := sha256Digest(body)
	if got != digest {
		writeRegistryError(w, http.StatusBadRequest, "DIGEST_INVALID", "digest does not match content")
		return
	}
	if err := s.Store.PutArRegistryBlob(digest, body); err != nil {
		writeRegistryError(w, http.StatusInternalServerError, "UNKNOWN", err.Error())
		return
	}
	if err := s.Store.LinkArRegistryBlob(digest, name); err != nil {
		writeRegistryError(w, http.StatusInternalServerError, "UNKNOWN", err.Error())
		return
	}
	s.uploadsMu.Lock()
	delete(s.uploads, uploadID)
	s.uploadsMu.Unlock()
	w.Header().Set("Location", "/v2/"+name+"/blobs/"+digest)
	w.Header().Set("Docker-Content-Digest", digest)
	w.WriteHeader(http.StatusCreated)
}

func (s *Service) registryPutManifest(w http.ResponseWriter, r *http.Request, p authn.Principal, name, reference string) {
	if err := s.requireRegistry(p, name, true); err != nil {
		writeRegistryAuthz(w, err)
		return
	}
	if name == "" || reference == "" {
		writeRegistryError(w, http.StatusBadRequest, "MANIFEST_INVALID", "name and reference required")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRegistryBlobBytes+1))
	if err != nil {
		writeRegistryError(w, http.StatusBadRequest, "MANIFEST_INVALID", "read manifest")
		return
	}
	if len(body) > maxRegistryBlobBytes {
		writeRegistryError(w, http.StatusRequestEntityTooLarge, "MANIFEST_INVALID", "manifest too large")
		return
	}
	digest := sha256Digest(body)
	if hdr := normalizeDigest(r.Header.Get("Docker-Content-Digest")); hdr != "" && hdr != digest {
		writeRegistryError(w, http.StatusBadRequest, "DIGEST_INVALID", "Docker-Content-Digest does not match body")
		return
	}
	mediaType := r.Header.Get("Content-Type")
	if mediaType == "" {
		mediaType = defaultManifestType
	}
	if err := s.Store.PutArRegistryBlob(digest, body); err != nil {
		writeRegistryError(w, http.StatusInternalServerError, "UNKNOWN", err.Error())
		return
	}
	if err := s.Store.LinkArRegistryBlob(digest, name); err != nil {
		writeRegistryError(w, http.StatusInternalServerError, "UNKNOWN", err.Error())
		return
	}
	m := store.ArRegistryManifest{
		Name: name, Reference: reference, Digest: digest, MediaType: mediaType, Data: body,
	}
	if err := s.Store.PutArRegistryManifest(m); err != nil {
		writeRegistryError(w, http.StatusInternalServerError, "UNKNOWN", err.Error())
		return
	}
	if reference != digest {
		m.Reference = digest
		if err := s.Store.PutArRegistryManifest(m); err != nil {
			writeRegistryError(w, http.StatusInternalServerError, "UNKNOWN", err.Error())
			return
		}
	}
	if err := s.upsertRegistryMetadata(name, reference, digest); err != nil {
		writeRegistryError(w, http.StatusInternalServerError, "UNKNOWN", err.Error())
		return
	}
	w.Header().Set("Location", "/v2/"+name+"/manifests/"+reference)
	w.Header().Set("Docker-Content-Digest", digest)
	w.WriteHeader(http.StatusCreated)
}

func (s *Service) upsertRegistryMetadata(imageName, reference, digest string) error {
	project := s.registryProject(imageName)
	repoID, pkgID := splitRegistryImage(imageName, project)
	repo := repoName(project, DefaultLocation, repoID)
	_, err := s.Store.CreateArRepository(store.ArRepository{
		Name: repo, ProjectID: project, Location: DefaultLocation, RepositoryID: repoID, Format: "DOCKER",
	})
	if err != nil {
		return err
	}
	pkg := packageName(repo, pkgID)
	if err := s.Store.UpsertArPackage(store.ArPackage{
		Name: pkg, RepositoryName: repo, PackageID: pkgID, DisplayName: strings.ReplaceAll(pkgID, "%2F", "/"),
	}); err != nil {
		return err
	}
	tagsJSON := "[]"
	if !validDigest(reference) {
		raw, _ := json.Marshal([]string{reference})
		tagsJSON = string(raw)
	}
	return s.Store.UpsertArVersion(store.ArVersion{
		Name: versionName(pkg, digest), PackageName: pkg, VersionID: digest,
		Description: digest, RelatedTagsJSON: tagsJSON,
	})
}

func (s *Service) requireRegistry(p authn.Principal, imageName string, push bool) error {
	project := s.registryProject(imageName)
	perms := registryPullPerms
	if push {
		perms = registryPushPerms
	}
	return s.requireAny(p, project, perms...)
}

func (s *Service) registryProject(imageName string) string {
	first, _, _ := strings.Cut(imageName, "/")
	if first == "" {
		return config.DefaultProjectID
	}
	if _, ok, err := s.Store.GetProject(first); err == nil && ok {
		return first
	}
	return config.DefaultProjectID
}

func splitRegistryImage(imageName, project string) (repoID, pkgID string) {
	rest := imageName
	if project != "" && (imageName == project || strings.HasPrefix(imageName, project+"/")) {
		rest = strings.TrimPrefix(imageName, project)
		rest = strings.TrimPrefix(rest, "/")
	}
	if rest == "" {
		return "library", "library"
	}
	parts := strings.Split(rest, "/")
	if len(parts) == 1 {
		return parts[0], parts[0]
	}
	return parts[0], strings.Join(parts[1:], "%2F")
}

func parseRegistryV2Rest(rest string) (registryV2Op, bool) {
	rest = strings.Trim(rest, "/")
	if rest == "" {
		return registryV2Op{}, false
	}
	const uploadsInfix = "/blobs/uploads/"
	if i := strings.LastIndex(rest, uploadsInfix); i >= 0 {
		name := rest[:i]
		id := strings.Trim(rest[i+len(uploadsInfix):], "/")
		if name == "" {
			return registryV2Op{}, false
		}
		if id == "" {
			return registryV2Op{name: name, kind: "uploads-start"}, true
		}
		return registryV2Op{name: name, kind: "uploads-put", id: id}, true
	}
	if strings.HasSuffix(rest, "/blobs/uploads") {
		name := strings.TrimSuffix(rest, "/blobs/uploads")
		name = strings.Trim(name, "/")
		if name == "" {
			return registryV2Op{}, false
		}
		return registryV2Op{name: name, kind: "uploads-start"}, true
	}
	const blobsInfix = "/blobs/"
	if i := strings.LastIndex(rest, blobsInfix); i >= 0 {
		name := rest[:i]
		id := rest[i+len(blobsInfix):]
		if name != "" && id != "" && !strings.HasPrefix(id, "uploads") {
			return registryV2Op{name: name, kind: "blob", id: id}, true
		}
	}
	const manifestsInfix = "/manifests/"
	if i := strings.LastIndex(rest, manifestsInfix); i >= 0 {
		name := rest[:i]
		id := rest[i+len(manifestsInfix):]
		if name != "" && id != "" {
			return registryV2Op{name: name, kind: "manifest", id: id}, true
		}
	}
	return registryV2Op{}, false
}

func sha256Digest(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func normalizeDigest(d string) string {
	d = strings.TrimSpace(d)
	if d == "" {
		return ""
	}
	lower := strings.ToLower(d)
	if strings.HasPrefix(lower, "sha256:") {
		return "sha256:" + strings.ToLower(d[len("sha256:"):])
	}
	return lower
}

func validDigest(d string) bool {
	if !strings.HasPrefix(d, "sha256:") {
		return false
	}
	hexPart := d[len("sha256:"):]
	if len(hexPart) != 64 {
		return false
	}
	for _, c := range hexPart {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func writeRegistryUnauthorized(w http.ResponseWriter) {
	w.Header().Set(registryVersionHeader, registryAPIVersion)
	w.Header().Set("WWW-Authenticate", "Bearer")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"errors": []map[string]string{{"code": "UNAUTHORIZED", "message": "authentication required"}},
	})
}

func writeRegistryAuthz(w http.ResponseWriter, err error) {
	if err == errDenied {
		writeRegistryError(w, http.StatusForbidden, "DENIED", "requested access to the resource is denied")
		return
	}
	writeRegistryError(w, http.StatusInternalServerError, "UNKNOWN", err.Error())
}

func writeRegistryError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set(registryVersionHeader, registryAPIVersion)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"errors": []map[string]string{{"code": code, "message": message}},
	})
}
