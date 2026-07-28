package gcs

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// PrincipalFunc extracts the authenticated principal from request context.
type PrincipalFunc func(r *http.Request) (authn.Principal, bool)

// Handler serves Cloud Storage JSON API v1 lab routes.
type Handler struct {
	Store          *store.Store
	Authz          *authz.Evaluator
	Principal      PrincipalFunc
	DefaultProject string
}

// Register mounts Storage JSON API routes on mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /storage/v1/b", h.createBucket)
	mux.HandleFunc("GET /storage/v1/b", h.listBuckets)
	mux.HandleFunc("GET /storage/v1/b/{bucket}", h.getBucket)
	mux.HandleFunc("DELETE /storage/v1/b/{bucket}", h.deleteBucket)
	mux.HandleFunc("GET /storage/v1/b/{bucket}/o", h.listObjects)
	mux.HandleFunc("GET /storage/v1/b/{bucket}/o/{object...}", h.getOrDownloadObject)
	mux.HandleFunc("DELETE /storage/v1/b/{bucket}/o/{object...}", h.deleteObject)
	mux.HandleFunc("POST /upload/storage/v1/b/{bucket}/o", h.uploadObject)
}

func (h *Handler) require(w http.ResponseWriter, r *http.Request, permission, resource string) (authn.Principal, bool) {
	p, ok := h.Principal(r)
	if !ok {
		gcperrors.Unauthenticated(w, "")
		return authn.Principal{}, false
	}
	allowed, err := h.Authz.Evaluate(p.Email, p.IsRoot, permission, resource)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return authn.Principal{}, false
	}
	if !allowed {
		gcperrors.PermissionDenied(w, "")
		return authn.Principal{}, false
	}
	return p, true
}

func projectResource(project string) string {
	return "projects/" + project
}

func (h *Handler) authProject(known string) string {
	if known != "" {
		return known
	}
	if h.DefaultProject != "" {
		return h.DefaultProject
	}
	return "unknown"
}

func (h *Handler) createBucket(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	if project == "" {
		gcperrors.InvalidArgument(w, "project query parameter is required")
		return
	}
	if _, ok := h.require(w, r, "storage.buckets.create", projectResource(project)); !ok {
		return
	}
	var body struct {
		Name         string `json:"name"`
		Location     string `json:"location"`
		StorageClass string `json:"storageClass"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid bucket body")
		return
	}
	if body.Name == "" {
		gcperrors.InvalidArgument(w, "bucket name is required")
		return
	}
	b, created, err := h.Store.CreateBucket(body.Name, project, body.Location, body.StorageClass)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "bucket already exists")
		return
	}
	writeJSON(w, http.StatusOK, bucketJSON(b))
}

func (h *Handler) listBuckets(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	if project == "" {
		gcperrors.InvalidArgument(w, "project query parameter is required")
		return
	}
	if _, ok := h.require(w, r, "storage.buckets.list", projectResource(project)); !ok {
		return
	}
	list, err := h.Store.ListBuckets(project)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for i := range list {
		items = append(items, bucketJSON(&list[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"kind": "storage#buckets", "items": items})
}

func (h *Handler) getBucket(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("bucket")
	b, ok, err := h.Store.GetBucket(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		if _, aok := h.require(w, r, "storage.buckets.get", projectResource(h.authProject(""))); !aok {
			return
		}
		gcperrors.NotFound(w, "bucket not found")
		return
	}
	if _, aok := h.require(w, r, "storage.buckets.get", projectResource(b.ProjectID)); !aok {
		return
	}
	writeJSON(w, http.StatusOK, bucketJSON(b))
}

func (h *Handler) deleteBucket(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("bucket")
	b, ok, err := h.Store.GetBucket(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		if _, aok := h.require(w, r, "storage.buckets.delete", projectResource(h.authProject(""))); !aok {
			return
		}
		gcperrors.NotFound(w, "bucket not found")
		return
	}
	if _, aok := h.require(w, r, "storage.buckets.delete", projectResource(b.ProjectID)); !aok {
		return
	}
	found, err := h.Store.DeleteBucket(name)
	if err != nil {
		if strings.Contains(err.Error(), "not empty") {
			gcperrors.WriteREST(w, http.StatusBadRequest, gcperrors.StatusFailedPrecondition, "bucket not empty")
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !found {
		gcperrors.NotFound(w, "bucket not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listObjects(w http.ResponseWriter, r *http.Request) {
	bucket := r.PathValue("bucket")
	b, ok, err := h.Store.GetBucket(bucket)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		if _, aok := h.require(w, r, "storage.objects.list", projectResource(h.authProject(""))); !aok {
			return
		}
		gcperrors.NotFound(w, "bucket not found")
		return
	}
	if _, aok := h.require(w, r, "storage.objects.list", projectResource(b.ProjectID)); !aok {
		return
	}
	prefix := r.URL.Query().Get("prefix")
	list, err := h.Store.ListObjects(bucket, prefix)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for i := range list {
		items = append(items, objectJSON(&list[i]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"kind": "storage#objects", "items": items})
}

func (h *Handler) getOrDownloadObject(w http.ResponseWriter, r *http.Request) {
	bucket := r.PathValue("bucket")
	object := r.PathValue("object")
	b, ok, err := h.Store.GetBucket(bucket)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		if _, aok := h.require(w, r, "storage.objects.get", projectResource(h.authProject(""))); !aok {
			return
		}
		gcperrors.NotFound(w, "bucket not found")
		return
	}
	if _, aok := h.require(w, r, "storage.objects.get", projectResource(b.ProjectID)); !aok {
		return
	}
	var gen int64
	if g := r.URL.Query().Get("generation"); g != "" {
		gen, err = strconv.ParseInt(g, 10, 64)
		if err != nil {
			gcperrors.InvalidArgument(w, "invalid generation")
			return
		}
	}
	obj, ok, err := h.Store.GetObject(bucket, object, gen)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "object not found")
		return
	}
	if r.URL.Query().Get("alt") == "media" {
		data, err := h.Store.ReadObjectBytes(obj)
		if err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		}
		w.Header().Set("Content-Type", obj.ContentType)
		w.Header().Set("X-Goog-Generation", strconv.FormatInt(obj.Generation, 10))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
		return
	}
	writeJSON(w, http.StatusOK, objectJSON(obj))
}

func (h *Handler) deleteObject(w http.ResponseWriter, r *http.Request) {
	bucket := r.PathValue("bucket")
	object := r.PathValue("object")
	b, ok, err := h.Store.GetBucket(bucket)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		if _, aok := h.require(w, r, "storage.objects.delete", projectResource(h.authProject(""))); !aok {
			return
		}
		gcperrors.NotFound(w, "bucket not found")
		return
	}
	if _, aok := h.require(w, r, "storage.objects.delete", projectResource(b.ProjectID)); !aok {
		return
	}
	var gen int64
	if g := r.URL.Query().Get("generation"); g != "" {
		gen, err = strconv.ParseInt(g, 10, 64)
		if err != nil {
			gcperrors.InvalidArgument(w, "invalid generation")
			return
		}
	}
	found, err := h.Store.DeleteObject(bucket, object, gen)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !found {
		gcperrors.NotFound(w, "object not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) uploadObject(w http.ResponseWriter, r *http.Request) {
	bucket := r.PathValue("bucket")
	b, ok, err := h.Store.GetBucket(bucket)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		if _, aok := h.require(w, r, "storage.objects.create", projectResource(h.authProject(""))); !aok {
			return
		}
		gcperrors.NotFound(w, "bucket not found")
		return
	}
	if _, aok := h.require(w, r, "storage.objects.create", projectResource(b.ProjectID)); !aok {
		return
	}
	uploadType := r.URL.Query().Get("uploadType")
	name := r.URL.Query().Get("name")
	contentType := r.Header.Get("Content-Type")
	var data []byte
	switch uploadType {
	case "", "media":
		if name == "" {
			gcperrors.InvalidArgument(w, "name query parameter is required for media upload")
			return
		}
		data, err = io.ReadAll(io.LimitReader(r.Body, 64<<20))
		if err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		}
		if contentType == "" {
			contentType = "application/octet-stream"
		}
	case "multipart":
		mediaType, params, err := mime.ParseMediaType(contentType)
		if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
			gcperrors.InvalidArgument(w, "multipart Content-Type required")
			return
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		var meta struct {
			Name         string `json:"name"`
			ContentType  string `json:"contentType"`
			ContentType2 string `json:"content_type"`
		}
		partIdx := 0
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				gcperrors.InvalidArgument(w, "invalid multipart body")
				return
			}
			body, err := io.ReadAll(io.LimitReader(part, 64<<20))
			_ = part.Close()
			if err != nil {
				gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
				return
			}
			if partIdx == 0 {
				if err := json.Unmarshal(body, &meta); err != nil {
					gcperrors.InvalidArgument(w, "invalid object metadata JSON")
					return
				}
				name = meta.Name
				if name == "" {
					name = r.URL.Query().Get("name")
				}
				contentType = meta.ContentType
				if contentType == "" {
					contentType = meta.ContentType2
				}
			} else {
				data = body
				if ct := part.Header.Get("Content-Type"); ct != "" && contentType == "" {
					contentType = ct
				}
			}
			partIdx++
		}
		if name == "" {
			gcperrors.InvalidArgument(w, "object name is required")
			return
		}
		if contentType == "" {
			contentType = "application/octet-stream"
		}
	default:
		gcperrors.InvalidArgument(w, fmt.Sprintf("unsupported uploadType %q", uploadType))
		return
	}
	obj, err := h.Store.PutObjectBytes(bucket, name, contentType, data)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, objectJSON(obj))
}

func bucketJSON(b *store.Bucket) map[string]any {
	return map[string]any{
		"kind":         "storage#bucket",
		"id":           b.Name,
		"name":         b.Name,
		"location":     b.Location,
		"storageClass": b.StorageClass,
		"timeCreated":  b.CreatedAt,
		"updated":      b.CreatedAt,
		"selfLink":     "/storage/v1/b/" + b.Name,
	}
}

func objectJSON(o *store.ObjectMeta) map[string]any {
	return map[string]any{
		"kind":         "storage#object",
		"id":           o.Bucket + "/" + o.Name + "/" + strconv.FormatInt(o.Generation, 10),
		"name":         o.Name,
		"bucket":       o.Bucket,
		"generation":   strconv.FormatInt(o.Generation, 10),
		"size":         strconv.FormatInt(o.Size, 10),
		"contentType":  o.ContentType,
		"md5Hash":      o.MD5Hash,
		"crc32c":       o.CRC32C,
		"timeCreated":  o.CreatedAt,
		"updated":      o.UpdatedAt,
		"storageClass": "STANDARD",
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
