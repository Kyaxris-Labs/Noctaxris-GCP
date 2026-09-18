package gcs

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

func (h *Handler) requireXMLHMAC(w http.ResponseWriter, r *http.Request) (authn.Principal, bool) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(strings.TrimSpace(auth), store.LabGCSSignAlgo) {
		gcperrors.Unauthenticated(w, "GOOG4-HMAC-SHA256 Authorization required")
		return authn.Principal{}, false
	}
	accessID := store.GOOG4AccessID(auth)
	secret, ok, err := h.Store.HMACSecret(accessID)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return authn.Principal{}, false
	}
	if !ok {
		gcperrors.Unauthenticated(w, "unknown HMAC access id")
		return authn.Principal{}, false
	}
	host := r.Host
	if host == "" {
		host = "127.0.0.1:4588"
	}
	googDate := r.Header.Get("x-goog-date")
	if googDate == "" {
		googDate = r.Header.Get("X-Goog-Date")
	}
	if err := store.VerifyGOOG4HMACHeader(r.Method, host, r.URL.Path, auth, googDate, secret, time.Time{}); err != nil {
		gcperrors.Unauthenticated(w, "invalid HMAC signature: "+err.Error())
		return authn.Principal{}, false
	}
	return authn.Principal{Email: "hmac:" + accessID, IsRoot: false}, true
}

func (h *Handler) xmlListBucket(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireXMLHMAC(w, r); !ok {
		return
	}
	bucket := r.PathValue("bucket")
	if _, ok, err := h.Store.GetBucket(bucket); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "bucket not found")
		return
	}
	prefix := r.URL.Query().Get("prefix")
	versions := strings.EqualFold(r.URL.Query().Get("versions"), "true") || r.URL.Query().Get("versions") == "yes"
	var items []store.ObjectMeta
	var err error
	if versions {
		items, err = h.Store.ListObjectGenerations(bucket, prefix)
	} else {
		items, err = h.Store.ListObjects(bucket, prefix)
	}
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	type content struct {
		XMLName    xml.Name `xml:"Contents"`
		Key        string   `xml:"Key"`
		Size       int64    `xml:"Size"`
		Generation int64    `xml:"Generation"`
	}
	type listResult struct {
		XMLName  xml.Name  `xml:"ListBucketResult"`
		Xmlns    string    `xml:"xmlns,attr"`
		Name     string    `xml:"Name"`
		Prefix   string    `xml:"Prefix"`
		Contents []content `xml:"Contents"`
	}
	out := listResult{Xmlns: "http://doc.s3.amazonaws.com/2006-03-01", Name: bucket, Prefix: prefix}
	for _, o := range items {
		out.Contents = append(out.Contents, content{Key: o.Name, Size: o.Size, Generation: o.Generation})
	}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	enc := xml.NewEncoder(w)
	_ = enc.Encode(out)
}

func (h *Handler) xmlGetObject(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireXMLHMAC(w, r); !ok {
		return
	}
	bucket := r.PathValue("bucket")
	object := r.PathValue("object")
	var gen int64
	if g := r.URL.Query().Get("generation"); g != "" {
		n, err := strconv.ParseInt(g, 10, 64)
		if err != nil {
			gcperrors.InvalidArgument(w, "invalid generation")
			return
		}
		gen = n
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
	data, err := h.Store.ReadObjectBytes(obj)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	w.Header().Set("Content-Type", obj.ContentType)
	w.Header().Set("x-goog-generation", strconv.FormatInt(obj.Generation, 10))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (h *Handler) xmlPutObject(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.requireXMLHMAC(w, r); !ok {
		return
	}
	bucket := r.PathValue("bucket")
	object := r.PathValue("object")
	body, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		gcperrors.InvalidArgument(w, "read body")
		return
	}
	ct := r.Header.Get("Content-Type")
	obj, err := h.Store.PutObjectBytes(bucket, object, ct, body)
	if err != nil {
		if strings.Contains(err.Error(), "bucket not found") {
			gcperrors.NotFound(w, "bucket not found")
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	w.Header().Set("x-goog-generation", strconv.FormatInt(obj.Generation, 10))
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) createHMACKey(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	sa := r.URL.Query().Get("serviceAccountEmail")
	if _, ok := h.requireStorage(w, r, "storage.hmacKeys.create", "", project); !ok {
		return
	}
	if sa == "" {
		gcperrors.InvalidArgument(w, "serviceAccountEmail is required")
		return
	}
	k, err := h.Store.CreateHMACKey(project, sa)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"kind":    "storage#hmacKey",
		"metadata": hmacMeta(k, false),
		"secret":  k.Secret,
	})
}

func (h *Handler) listHMACKeys(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	if _, ok := h.requireStorage(w, r, "storage.hmacKeys.list", "", project); !ok {
		return
	}
	list, err := h.Store.ListHMACKeys(project)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for i := range list {
		items = append(items, hmacMeta(&list[i], true))
	}
	writeJSON(w, http.StatusOK, map[string]any{"kind": "storage#hmacKeysMetadata", "items": items})
}

func (h *Handler) getHMACKey(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	if _, ok := h.requireStorage(w, r, "storage.hmacKeys.get", "", project); !ok {
		return
	}
	k, ok, err := h.Store.GetHMACKey(r.PathValue("accessId"))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "hmac key not found")
		return
	}
	writeJSON(w, http.StatusOK, hmacMeta(k, true))
}

func (h *Handler) deleteHMACKey(w http.ResponseWriter, r *http.Request) {
	project := r.PathValue("project")
	if _, ok := h.requireStorage(w, r, "storage.hmacKeys.delete", "", project); !ok {
		return
	}
	ok, err := h.Store.DeleteHMACKey(r.PathValue("accessId"))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "hmac key not found")
		return
	}
	w.WriteHeader(http.StatusOK)
}

func hmacMeta(k *store.HMACKey, hideSecret bool) map[string]any {
	m := map[string]any{
		"kind":                "storage#hmacKeyMetadata",
		"id":                  k.AccessID,
		"selfLink":            fmt.Sprintf("/storage/v1/projects/%s/hmacKeys/%s", k.ProjectID, k.AccessID),
		"accessId":            k.AccessID,
		"projectId":           k.ProjectID,
		"serviceAccountEmail": k.ServiceAccountEmail,
		"state":               k.State,
		"timeCreated":         k.CreatedAt,
	}
	if !hideSecret && k.Secret != "" {
		m["secret"] = k.Secret
	}
	return m
}
