package gcs

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

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
	mux.HandleFunc("PATCH /storage/v1/b/{bucket}", h.patchBucket)
	mux.HandleFunc("DELETE /storage/v1/b/{bucket}", h.deleteBucket)
	mux.HandleFunc("GET /storage/v1/b/{bucket}/iam", h.getBucketIAM)
	mux.HandleFunc("PUT /storage/v1/b/{bucket}/iam", h.setBucketIAM)
	mux.HandleFunc("GET /storage/v1/b/{bucket}/iam/testPermissions", h.testBucketIAM)
	mux.HandleFunc("GET /storage/v1/b/{bucket}/o", h.listObjects)
	mux.HandleFunc("GET /storage/v1/b/{bucket}/o/{object...}", h.getOrDownloadObject)
	mux.HandleFunc("PATCH /storage/v1/b/{bucket}/o/{object...}", h.patchObject)
	mux.HandleFunc("DELETE /storage/v1/b/{bucket}/o/{object...}", h.deleteObject)
	mux.HandleFunc("POST /storage/v1/b/{bucket}/o/{object...}", h.postObjectAction)
	mux.HandleFunc("POST /upload/storage/v1/b/{bucket}/o", h.uploadObject)
	mux.HandleFunc("PUT /upload/storage/v1/b/{bucket}/o", h.putResumableUpload)
	mux.HandleFunc("DELETE /upload/storage/v1/b/{bucket}/o", h.deleteResumableUpload)
}

func (h *Handler) principal(r *http.Request) (authn.Principal, bool) {
	if h.Principal != nil {
		return h.Principal(r)
	}
	return authn.Principal{}, false
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

// requireStorage evaluates permission against bucket IAM and/or project IAM.
// When the request carries a lab V4 signed URL query (and no principal), signature
// verification substitutes for Bearer + IAM for the requested method.
func (h *Handler) requireStorage(w http.ResponseWriter, r *http.Request, permission, bucketName, projectID string) (authn.Principal, bool) {
	if p, ok := h.principal(r); ok {
		var resources []string
		if bucketName != "" {
			resources = append(resources, store.BucketIAMResource(bucketName))
		}
		if projectID != "" {
			resources = append(resources, projectResource(projectID))
		}
		allowed, err := h.Authz.EvaluateAny(p.Email, p.IsRoot, permission, resources...)
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
	if store.HasV4Signature(r.URL.Query()) {
		host := r.Host
		if host == "" {
			host = "127.0.0.1:4588"
		}
		if err := store.VerifyV4SignedRequest(r.Method, host, r.URL.Path, r.URL.Query(), time.Time{}); err != nil {
			gcperrors.Unauthenticated(w, "invalid signed URL: "+err.Error())
			return authn.Principal{}, false
		}
		return authn.Principal{Email: store.LabGCSHMACAccessID + "@lab.local", IsRoot: false}, true
	}
	gcperrors.Unauthenticated(w, "")
	return authn.Principal{}, false
}

func (h *Handler) createBucket(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	if project == "" {
		gcperrors.InvalidArgument(w, "project query parameter is required")
		return
	}
	if _, ok := h.requireStorage(w, r, "storage.buckets.create", "", project); !ok {
		return
	}
	var body struct {
		Name         string            `json:"name"`
		Location     string            `json:"location"`
		StorageClass string            `json:"storageClass"`
		Labels       map[string]string `json:"labels"`
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
	if len(body.Labels) > 0 {
		b, err = h.Store.PatchBucket(body.Name, nil, nil, &body.Labels, nil)
		if err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, bucketJSON(b))
}

func (h *Handler) listBuckets(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	if project == "" {
		gcperrors.InvalidArgument(w, "project query parameter is required")
		return
	}
	if _, ok := h.requireStorage(w, r, "storage.buckets.list", "", project); !ok {
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
		if _, aok := h.requireStorage(w, r, "storage.buckets.get", name, h.authProject("")); !aok {
			return
		}
		gcperrors.NotFound(w, "bucket not found")
		return
	}
	if _, aok := h.requireStorage(w, r, "storage.buckets.get", b.Name, b.ProjectID); !aok {
		return
	}
	writeJSON(w, http.StatusOK, bucketJSON(b))
}

func (h *Handler) patchBucket(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("bucket")
	b, ok, err := h.Store.GetBucket(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		if _, aok := h.requireStorage(w, r, "storage.buckets.update", name, h.authProject("")); !aok {
			return
		}
		gcperrors.NotFound(w, "bucket not found")
		return
	}
	if _, aok := h.requireStorage(w, r, "storage.buckets.update", b.Name, b.ProjectID); !aok {
		return
	}
	var body struct {
		Location        *string            `json:"location"`
		StorageClass    *string            `json:"storageClass"`
		Labels          *map[string]string `json:"labels"`
		RetentionPolicy *struct {
			RetentionPeriod json.RawMessage `json:"retentionPeriod"`
			IsLocked        *bool           `json:"isLocked"`
		} `json:"retentionPolicy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid bucket patch body")
		return
	}
	var retention *store.BucketRetentionPolicy
	if body.RetentionPolicy != nil {
		period, err := parseRetentionPeriod(body.RetentionPolicy.RetentionPeriod)
		if err != nil {
			gcperrors.InvalidArgument(w, "invalid retentionPeriod")
			return
		}
		retention = &store.BucketRetentionPolicy{RetentionPeriodSeconds: period}
		if body.RetentionPolicy.IsLocked != nil && *body.RetentionPolicy.IsLocked {
			retention.IsLocked = true
		}
	}
	updated, err := h.Store.PatchBucket(name, body.Location, body.StorageClass, body.Labels, retention)
	if err != nil {
		if err == store.ErrRetentionPolicyLocked {
			gcperrors.WriteREST(w, http.StatusBadRequest, gcperrors.StatusFailedPrecondition, "retention policy is locked")
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, bucketJSON(updated))
}

func (h *Handler) deleteBucket(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("bucket")
	b, ok, err := h.Store.GetBucket(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		if _, aok := h.requireStorage(w, r, "storage.buckets.delete", name, h.authProject("")); !aok {
			return
		}
		gcperrors.NotFound(w, "bucket not found")
		return
	}
	if _, aok := h.requireStorage(w, r, "storage.buckets.delete", b.Name, b.ProjectID); !aok {
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

func (h *Handler) getBucketIAM(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("bucket")
	b, ok, err := h.Store.GetBucket(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	project := h.authProject("")
	if ok {
		project = b.ProjectID
	}
	if _, aok := h.requireStorage(w, r, "storage.buckets.getIamPolicy", name, project); !aok {
		return
	}
	if !ok {
		gcperrors.NotFound(w, "bucket not found")
		return
	}
	raw, found, err := h.Store.GetIAMPolicyJSON(store.BucketIAMResource(name))
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

func (h *Handler) setBucketIAM(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("bucket")
	b, ok, err := h.Store.GetBucket(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	project := h.authProject("")
	if ok {
		project = b.ProjectID
	}
	if _, aok := h.requireStorage(w, r, "storage.buckets.setIamPolicy", name, project); !aok {
		return
	}
	if !ok {
		gcperrors.NotFound(w, "bucket not found")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		gcperrors.InvalidArgument(w, "unable to read body")
		return
	}
	var policy authz.Policy
	if err := json.Unmarshal(body, &policy); err != nil {
		// GCS setIamPolicy may wrap as {"bindings":...} directly (policy document).
		gcperrors.InvalidArgument(w, "invalid IAM policy body")
		return
	}
	if err := h.Store.PutIAMPolicyJSON(store.BucketIAMResource(name), policy); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, policy)
}

func (h *Handler) testBucketIAM(w http.ResponseWriter, r *http.Request) {
	p, ok := h.principal(r)
	if !ok {
		gcperrors.Unauthenticated(w, "")
		return
	}
	name := r.PathValue("bucket")
	b, found, err := h.Store.GetBucket(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	project := h.authProject("")
	if found {
		project = b.ProjectID
	}
	perms := r.URL.Query()["permissions"]
	if len(perms) == 1 && strings.Contains(perms[0], ",") {
		perms = strings.Split(perms[0], ",")
	}
	resources := []string{store.BucketIAMResource(name), projectResource(project)}
	granted, err := h.Authz.TestIamPermissionsAny(p.Email, p.IsRoot, resources, perms)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if granted == nil {
		granted = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"kind": "storage#testIamPermissionsResponse", "permissions": granted})
}

func (h *Handler) listObjects(w http.ResponseWriter, r *http.Request) {
	bucket := r.PathValue("bucket")
	b, ok, err := h.Store.GetBucket(bucket)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		if _, aok := h.requireStorage(w, r, "storage.objects.list", bucket, h.authProject("")); !aok {
			return
		}
		gcperrors.NotFound(w, "bucket not found")
		return
	}
	if _, aok := h.requireStorage(w, r, "storage.objects.list", b.Name, b.ProjectID); !aok {
		return
	}
	prefix := r.URL.Query().Get("prefix")
	delimiter := r.URL.Query().Get("delimiter")
	result, err := h.Store.ListObjectsDelimited(bucket, prefix, delimiter)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(result.Items))
	for i := range result.Items {
		items = append(items, objectJSON(&result.Items[i]))
	}
	resp := map[string]any{"kind": "storage#objects", "items": items}
	if len(result.Prefixes) > 0 {
		resp["prefixes"] = result.Prefixes
	}
	writeJSON(w, http.StatusOK, resp)
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
		if _, aok := h.requireStorage(w, r, "storage.objects.get", bucket, h.authProject("")); !aok {
			return
		}
		gcperrors.NotFound(w, "bucket not found")
		return
	}
	if _, aok := h.requireStorage(w, r, "storage.objects.get", b.Name, b.ProjectID); !aok {
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
	if match := r.URL.Query().Get("ifGenerationMatch"); match != "" {
		want, err := strconv.ParseInt(match, 10, 64)
		if err != nil {
			gcperrors.InvalidArgument(w, "invalid ifGenerationMatch")
			return
		}
		if err := h.Store.CheckGenerationMatch(bucket, object, want); err != nil {
			if err == store.ErrPreconditionFailed {
				gcperrors.WriteREST(w, http.StatusPreconditionFailed, gcperrors.StatusFailedPrecondition, "ifGenerationMatch failed")
				return
			}
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
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

func (h *Handler) patchObject(w http.ResponseWriter, r *http.Request) {
	bucket := r.PathValue("bucket")
	object := r.PathValue("object")
	b, ok, err := h.Store.GetBucket(bucket)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		if _, aok := h.requireStorage(w, r, "storage.objects.update", bucket, h.authProject("")); !aok {
			return
		}
		gcperrors.NotFound(w, "bucket not found")
		return
	}
	if _, aok := h.requireStorage(w, r, "storage.objects.update", b.Name, b.ProjectID); !aok {
		return
	}
	var body struct {
		ContentType        string            `json:"contentType"`
		Metadata           map[string]string `json:"metadata"`
		CacheControl       string            `json:"cacheControl"`
		ContentDisposition string            `json:"contentDisposition"`
		ContentEncoding    string            `json:"contentEncoding"`
		ContentLanguage    string            `json:"contentLanguage"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid object patch body")
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
	patch := &store.ObjectMeta{
		ContentType:        body.ContentType,
		Metadata:           body.Metadata,
		CacheControl:       body.CacheControl,
		ContentDisposition: body.ContentDisposition,
		ContentEncoding:    body.ContentEncoding,
		ContentLanguage:    body.ContentLanguage,
	}
	obj, err := h.Store.PatchObjectMetadata(bucket, object, gen, patch)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			gcperrors.NotFound(w, "object not found")
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
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
		if _, aok := h.requireStorage(w, r, "storage.objects.delete", bucket, h.authProject("")); !aok {
			return
		}
		gcperrors.NotFound(w, "bucket not found")
		return
	}
	if _, aok := h.requireStorage(w, r, "storage.objects.delete", b.Name, b.ProjectID); !aok {
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
	if match := r.URL.Query().Get("ifGenerationMatch"); match != "" {
		want, err := strconv.ParseInt(match, 10, 64)
		if err != nil {
			gcperrors.InvalidArgument(w, "invalid ifGenerationMatch")
			return
		}
		if err := h.Store.CheckGenerationMatch(bucket, object, want); err != nil {
			if err == store.ErrPreconditionFailed {
				gcperrors.WriteREST(w, http.StatusPreconditionFailed, gcperrors.StatusFailedPrecondition, "ifGenerationMatch failed")
				return
			}
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		}
	}
	found, err := h.Store.DeleteObject(bucket, object, gen)
	if err != nil {
		if err == store.ErrRetentionPolicyNotMet {
			gcperrors.WriteREST(w, http.StatusBadRequest, gcperrors.StatusFailedPrecondition, "retention policy not met")
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !found {
		gcperrors.NotFound(w, "object not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) postObjectAction(w http.ResponseWriter, r *http.Request) {
	objectPath := r.PathValue("object")
	if strings.HasSuffix(objectPath, "/compose") {
		h.composeObject(w, r, strings.TrimSuffix(objectPath, "/compose"))
		return
	}
	if i := strings.Index(objectPath, "/copyTo/b/"); i >= 0 {
		srcObject := objectPath[:i]
		rest := objectPath[i+len("/copyTo/b/"):]
		dstBucket, dstObject, ok := strings.Cut(rest, "/o/")
		if !ok || dstBucket == "" || dstObject == "" {
			gcperrors.InvalidArgument(w, "invalid copyTo path")
			return
		}
		h.copyObject(w, r, srcObject, dstBucket, dstObject)
		return
	}
	if i := strings.Index(objectPath, "/rewriteTo/b/"); i >= 0 {
		srcObject := objectPath[:i]
		rest := objectPath[i+len("/rewriteTo/b/"):]
		dstBucket, dstObject, ok := strings.Cut(rest, "/o/")
		if !ok || dstBucket == "" || dstObject == "" {
			gcperrors.InvalidArgument(w, "invalid rewriteTo path")
			return
		}
		h.rewriteObject(w, r, srcObject, dstBucket, dstObject)
		return
	}
	if obj, action, ok := strings.Cut(objectPath, ":"); ok && action == "generateSignedUrl" {
		h.generateSignedURL(w, r, obj)
		return
	}
	gcperrors.InvalidArgument(w, "unsupported object POST action")
}

// generateSignedURL mints a lab V4 HMAC signed URL for GET/PUT against the JSON API path.
func (h *Handler) generateSignedURL(w http.ResponseWriter, r *http.Request, object string) {
	bucket := r.PathValue("bucket")
	b, ok, err := h.Store.GetBucket(bucket)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		if _, aok := h.requireStorage(w, r, "storage.objects.get", bucket, h.authProject("")); !aok {
			return
		}
		gcperrors.NotFound(w, "bucket not found")
		return
	}
	if _, aok := h.requireStorage(w, r, "storage.objects.get", b.Name, b.ProjectID); !aok {
		return
	}
	var body struct {
		Method  string `json:"method"`
		Expires int    `json:"expires"`
		Alt     string `json:"alt"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	method := strings.ToUpper(strings.TrimSpace(body.Method))
	if method == "" {
		method = "GET"
	}
	if method != "GET" && method != "PUT" {
		gcperrors.InvalidArgument(w, "method must be GET or PUT")
		return
	}
	host := r.Host
	if host == "" {
		host = "127.0.0.1:4588"
	}
	var path string
	q := url.Values{}
	if method == "PUT" {
		path = "/upload/storage/v1/b/" + url.PathEscape(bucket) + "/o"
		q.Set("uploadType", "media")
		q.Set("name", object)
	} else {
		path = "/storage/v1/b/" + url.PathEscape(bucket) + "/o/" + objectPathEscape(object)
		alt := body.Alt
		if alt == "" {
			alt = "media"
		}
		if alt != "" {
			q.Set("alt", alt)
		}
	}
	signed, err := store.GenerateV4SignedURL(store.SignedURLRequest{
		Method:  method,
		Host:    host,
		Path:    path,
		Expires: body.Expires,
		Query:   q,
	})
	if err != nil {
		gcperrors.InvalidArgument(w, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"signedUrl": signed,
		"algorithm": store.LabGCSSignAlgo,
		"accessId":  store.LabGCSHMACAccessID,
	})
}

func objectPathEscape(object string) string {
	parts := strings.Split(object, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

func (h *Handler) composeObject(w http.ResponseWriter, r *http.Request, dest string) {
	bucket := r.PathValue("bucket")
	b, ok, err := h.Store.GetBucket(bucket)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		if _, aok := h.requireStorage(w, r, "storage.objects.create", bucket, h.authProject("")); !aok {
			return
		}
		gcperrors.NotFound(w, "bucket not found")
		return
	}
	if _, aok := h.requireStorage(w, r, "storage.objects.create", b.Name, b.ProjectID); !aok {
		return
	}
	var body struct {
		SourceObjects []struct {
			Name string `json:"name"`
		} `json:"sourceObjects"`
		Destination struct {
			ContentType string `json:"contentType"`
		} `json:"destination"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid compose body")
		return
	}
	sources := make([]string, 0, len(body.SourceObjects))
	for _, s := range body.SourceObjects {
		if s.Name != "" {
			sources = append(sources, s.Name)
		}
	}
	obj, err := h.Store.ComposeObject(bucket, dest, sources, body.Destination.ContentType)
	if err != nil {
		if err == store.ErrRetentionPolicyNotMet {
			gcperrors.WriteREST(w, http.StatusBadRequest, gcperrors.StatusFailedPrecondition, "retention policy not met")
			return
		}
		if strings.Contains(err.Error(), "at most 32") {
			gcperrors.InvalidArgument(w, err.Error())
			return
		}
		if strings.Contains(err.Error(), "not found") {
			gcperrors.NotFound(w, err.Error())
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, objectJSON(obj))
}

func (h *Handler) copyObject(w http.ResponseWriter, r *http.Request, srcObject, dstBucket, dstObject string) {
	srcBucket := r.PathValue("bucket")
	sb, ok, err := h.Store.GetBucket(srcBucket)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		if _, aok := h.requireStorage(w, r, "storage.objects.get", srcBucket, h.authProject("")); !aok {
			return
		}
		gcperrors.NotFound(w, "source bucket not found")
		return
	}
	if _, aok := h.requireStorage(w, r, "storage.objects.get", sb.Name, sb.ProjectID); !aok {
		return
	}
	db, ok, err := h.Store.GetBucket(dstBucket)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		if _, aok := h.requireStorage(w, r, "storage.objects.create", dstBucket, h.authProject("")); !aok {
			return
		}
		gcperrors.NotFound(w, "destination bucket not found")
		return
	}
	if _, aok := h.requireStorage(w, r, "storage.objects.create", db.Name, db.ProjectID); !aok {
		return
	}
	var gen int64
	if g := r.URL.Query().Get("sourceGeneration"); g != "" {
		gen, err = strconv.ParseInt(g, 10, 64)
		if err != nil {
			gcperrors.InvalidArgument(w, "invalid sourceGeneration")
			return
		}
	}
	obj, err := h.Store.CopyObject(srcBucket, srcObject, gen, dstBucket, dstObject)
	if err != nil {
		if err == store.ErrRetentionPolicyNotMet {
			gcperrors.WriteREST(w, http.StatusBadRequest, gcperrors.StatusFailedPrecondition, "retention policy not met")
			return
		}
		if strings.Contains(err.Error(), "not found") {
			gcperrors.NotFound(w, err.Error())
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, objectJSON(obj))
}

func (h *Handler) rewriteObject(w http.ResponseWriter, r *http.Request, srcObject, dstBucket, dstObject string) {
	srcBucket := r.PathValue("bucket")
	sb, ok, err := h.Store.GetBucket(srcBucket)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		if _, aok := h.requireStorage(w, r, "storage.objects.get", srcBucket, h.authProject("")); !aok {
			return
		}
		gcperrors.NotFound(w, "source bucket not found")
		return
	}
	if _, aok := h.requireStorage(w, r, "storage.objects.get", sb.Name, sb.ProjectID); !aok {
		return
	}
	db, ok, err := h.Store.GetBucket(dstBucket)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		if _, aok := h.requireStorage(w, r, "storage.objects.create", dstBucket, h.authProject("")); !aok {
			return
		}
		gcperrors.NotFound(w, "destination bucket not found")
		return
	}
	if _, aok := h.requireStorage(w, r, "storage.objects.create", db.Name, db.ProjectID); !aok {
		return
	}
	var gen int64
	if g := r.URL.Query().Get("sourceGeneration"); g != "" {
		gen, err = strconv.ParseInt(g, 10, 64)
		if err != nil {
			gcperrors.InvalidArgument(w, "invalid sourceGeneration")
			return
		}
	}
	obj, err := h.Store.RewriteObject(srcBucket, srcObject, gen, dstBucket, dstObject)
	if err != nil {
		if err == store.ErrRetentionPolicyNotMet {
			gcperrors.WriteREST(w, http.StatusBadRequest, gcperrors.StatusFailedPrecondition, "retention policy not met")
			return
		}
		if strings.Contains(err.Error(), "not found") {
			gcperrors.NotFound(w, err.Error())
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"kind":                "storage#rewriteResponse",
		"totalBytesRewritten": strconv.FormatInt(obj.Size, 10),
		"objectSize":          strconv.FormatInt(obj.Size, 10),
		"done":                true,
		"resource":            objectJSON(obj),
	})
}

func (h *Handler) uploadObject(w http.ResponseWriter, r *http.Request) {
	bucket := r.PathValue("bucket")
	b, ok, err := h.Store.GetBucket(bucket)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		if _, aok := h.requireStorage(w, r, "storage.objects.create", bucket, h.authProject("")); !aok {
			return
		}
		gcperrors.NotFound(w, "bucket not found")
		return
	}
	if _, aok := h.requireStorage(w, r, "storage.objects.create", b.Name, b.ProjectID); !aok {
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
	case "resumable":
		h.initiateResumable(w, r, bucket, name)
		return
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
		if err == store.ErrRetentionPolicyNotMet {
			gcperrors.WriteREST(w, http.StatusBadRequest, gcperrors.StatusFailedPrecondition, "retention policy not met")
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, objectJSON(obj))
}

func (h *Handler) initiateResumable(w http.ResponseWriter, r *http.Request, bucket, name string) {
	contentType := r.Header.Get("X-Upload-Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if name == "" {
		var meta struct {
			Name        string `json:"name"`
			ContentType string `json:"contentType"`
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		}
		if len(body) > 0 {
			_ = json.Unmarshal(body, &meta)
			name = meta.Name
			if meta.ContentType != "" {
				contentType = meta.ContentType
			}
		}
	}
	if name == "" {
		gcperrors.InvalidArgument(w, "object name is required for resumable upload")
		return
	}
	sess, err := h.Store.CreateUploadSession(bucket, name, contentType)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	loc := fmt.Sprintf("/upload/storage/v1/b/%s/o?uploadType=resumable&upload_id=%s", bucket, sess.UploadID)
	w.Header().Set("Location", loc)
	w.Header().Set("X-GUploader-UploadID", sess.UploadID)
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) putResumableUpload(w http.ResponseWriter, r *http.Request) {
	bucket := r.PathValue("bucket")
	// Lab media PUT theatre (signed URL uploads): uploadType=media&name=...
	if r.URL.Query().Get("uploadType") == "media" {
		h.putMediaUpload(w, r, bucket)
		return
	}
	uploadID := r.URL.Query().Get("upload_id")
	if uploadID == "" {
		gcperrors.InvalidArgument(w, "upload_id is required")
		return
	}
	sess, ok, err := h.Store.GetUploadSession(uploadID)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok || sess.Bucket != bucket {
		gcperrors.NotFound(w, "upload session not found")
		return
	}
	b, bok, err := h.Store.GetBucket(bucket)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !bok {
		gcperrors.NotFound(w, "bucket not found")
		return
	}
	if _, aok := h.requireStorage(w, r, "storage.objects.create", b.Name, b.ProjectID); !aok {
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 64<<20))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	obj, err := h.Store.CompleteUploadSession(uploadID, data)
	if err != nil {
		if err == store.ErrRetentionPolicyNotMet {
			gcperrors.WriteREST(w, http.StatusBadRequest, gcperrors.StatusFailedPrecondition, "retention policy not met")
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, objectJSON(obj))
}

func (h *Handler) putMediaUpload(w http.ResponseWriter, r *http.Request, bucket string) {
	name := r.URL.Query().Get("name")
	if name == "" {
		gcperrors.InvalidArgument(w, "name query parameter is required for media upload")
		return
	}
	b, ok, err := h.Store.GetBucket(bucket)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		if _, aok := h.requireStorage(w, r, "storage.objects.create", bucket, h.authProject("")); !aok {
			return
		}
		gcperrors.NotFound(w, "bucket not found")
		return
	}
	if _, aok := h.requireStorage(w, r, "storage.objects.create", b.Name, b.ProjectID); !aok {
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 64<<20))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	obj, err := h.Store.PutObjectBytes(bucket, name, contentType, data)
	if err != nil {
		if err == store.ErrRetentionPolicyNotMet {
			gcperrors.WriteREST(w, http.StatusBadRequest, gcperrors.StatusFailedPrecondition, "retention policy not met")
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, objectJSON(obj))
}

func (h *Handler) deleteResumableUpload(w http.ResponseWriter, r *http.Request) {
	uploadID := r.URL.Query().Get("upload_id")
	if uploadID == "" {
		gcperrors.InvalidArgument(w, "upload_id is required")
		return
	}
	bucket := r.PathValue("bucket")
	b, ok, err := h.Store.GetBucket(bucket)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	project := h.authProject("")
	if ok {
		project = b.ProjectID
	}
	if _, aok := h.requireStorage(w, r, "storage.objects.delete", bucket, project); !aok {
		return
	}
	found, err := h.Store.DeleteUploadSession(uploadID)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !found {
		gcperrors.NotFound(w, "upload session not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func bucketJSON(b *store.Bucket) map[string]any {
	labels := b.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	updated := b.UpdatedAt
	if updated == "" {
		updated = b.CreatedAt
	}
	out := map[string]any{
		"kind":           "storage#bucket",
		"id":             b.Name,
		"name":           b.Name,
		"location":       b.Location,
		"storageClass":   b.StorageClass,
		"labels":         labels,
		"metageneration": strconv.FormatInt(b.Metageneration, 10),
		"timeCreated":    b.CreatedAt,
		"updated":        updated,
		"selfLink":       "/storage/v1/b/" + b.Name,
	}
	if b.RetentionPeriodSeconds > 0 {
		out["retentionPolicy"] = map[string]any{
			"retentionPeriod": strconv.FormatInt(b.RetentionPeriodSeconds, 10),
			"isLocked":        b.RetentionIsLocked,
			"effectiveTime":   b.RetentionEffectiveTime,
		}
	}
	return out
}

func parseRetentionPeriod(raw json.RawMessage) (int64, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, nil
	}
	var asNum int64
	if err := json.Unmarshal(raw, &asNum); err == nil {
		if asNum < 0 {
			return 0, fmt.Errorf("negative retentionPeriod")
		}
		return asNum, nil
	}
	var asStr string
	if err := json.Unmarshal(raw, &asStr); err != nil {
		return 0, err
	}
	asStr = strings.TrimSpace(asStr)
	if asStr == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(asStr, 10, 64)
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, fmt.Errorf("negative retentionPeriod")
	}
	return n, nil
}

func objectJSON(o *store.ObjectMeta) map[string]any {
	meta := o.Metadata
	if meta == nil {
		meta = map[string]string{}
	}
	out := map[string]any{
		"kind":           "storage#object",
		"id":             o.Bucket + "/" + o.Name + "/" + strconv.FormatInt(o.Generation, 10),
		"name":           o.Name,
		"bucket":         o.Bucket,
		"generation":     strconv.FormatInt(o.Generation, 10),
		"metageneration": strconv.FormatInt(o.Metageneration, 10),
		"size":           strconv.FormatInt(o.Size, 10),
		"contentType":    o.ContentType,
		"md5Hash":        o.MD5Hash,
		"crc32c":         o.CRC32C,
		"metadata":       meta,
		"timeCreated":    o.CreatedAt,
		"updated":        o.UpdatedAt,
		"storageClass":   "STANDARD",
	}
	if o.CacheControl != "" {
		out["cacheControl"] = o.CacheControl
	}
	if o.ContentDisposition != "" {
		out["contentDisposition"] = o.ContentDisposition
	}
	if o.ContentEncoding != "" {
		out["contentEncoding"] = o.ContentEncoding
	}
	if o.ContentLanguage != "" {
		out["contentLanguage"] = o.ContentLanguage
	}
	return out
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
