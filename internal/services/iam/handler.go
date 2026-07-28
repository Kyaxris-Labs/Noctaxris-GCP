package iam

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// PrincipalFunc extracts the authenticated principal from a request context.
type PrincipalFunc func(r *http.Request) (authn.Principal, bool)

// Handler serves IAM Admin REST for service accounts and keys.
type Handler struct {
	Store     *store.Store
	Authz     *authz.Evaluator
	Principal PrincipalFunc
	Now       func() time.Time
}

// Mount registers IAM Admin routes on mux.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/projects/{project}/serviceAccounts", h.listServiceAccounts)
	mux.HandleFunc("POST /v1/projects/{project}/serviceAccounts", h.createServiceAccount)
	mux.HandleFunc("GET /v1/projects/{project}/serviceAccounts/{account}", h.getServiceAccount)
	mux.HandleFunc("PATCH /v1/projects/{project}/serviceAccounts/{account}", h.patchServiceAccount)
	mux.HandleFunc("DELETE /v1/projects/{project}/serviceAccounts/{account}", h.deleteServiceAccount)
	mux.HandleFunc("POST /v1/projects/{project}/serviceAccounts/{account}", h.serviceAccountPost)
	mux.HandleFunc("GET /v1/projects/{project}/serviceAccounts/{account}/keys", h.listKeys)
	mux.HandleFunc("POST /v1/projects/{project}/serviceAccounts/{account}/keys", h.createKey)
	mux.HandleFunc("GET /v1/projects/{project}/serviceAccounts/{account}/keys/{key}", h.getKey)
	mux.HandleFunc("DELETE /v1/projects/{project}/serviceAccounts/{account}/keys/{key}", h.deleteKey)
}

func (h *Handler) principal(r *http.Request) (authn.Principal, bool) {
	if h.Principal != nil {
		return h.Principal(r)
	}
	return authn.Principal{}, false
}

func (h *Handler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now().UTC()
}

func (h *Handler) require(w http.ResponseWriter, r *http.Request, permission, resource string) (authn.Principal, bool) {
	p, ok := h.principal(r)
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

func (h *Handler) createServiceAccount(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project")
	resource := "projects/" + projectID
	if _, ok := h.require(w, r, "iam.serviceAccounts.create", resource); !ok {
		return
	}
	// Optional Service Usage gate: refuse creates when iam.googleapis.com is DISABLED.
	enabled, err := h.Store.IsServiceEnabled(projectID, "iam.googleapis.com")
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !enabled {
		gcperrors.WriteREST(w, http.StatusBadRequest, gcperrors.StatusFailedPrecondition,
			"Service iam.googleapis.com is disabled for this project.")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		gcperrors.InvalidArgument(w, "unable to read body")
		return
	}
	var req struct {
		AccountID      string `json:"accountId"`
		ServiceAccount struct {
			DisplayName string `json:"displayName"`
			Description string `json:"description"`
		} `json:"serviceAccount"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	accountID := strings.TrimSpace(req.AccountID)
	if !validAccountID(accountID) {
		gcperrors.InvalidArgument(w, "accountId must be 6-30 characters matching [a-z]([-a-z0-9]*[a-z0-9])")
		return
	}
	email := accountID + "@" + projectID + ".iam.gserviceaccount.com"
	uniqueID := newUniqueID()
	created := h.now().Format(time.RFC3339Nano)
	err = h.Store.CreateServiceAccount(store.ServiceAccount{
		ProjectID:   projectID,
		Email:       email,
		UniqueID:    uniqueID,
		DisplayName: req.ServiceAccount.DisplayName,
		CreatedAt:   created,
	})
	if errors.Is(err, store.ErrAlreadyExists) {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "service account already exists")
		return
	}
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, saJSON(store.ServiceAccount{
		ProjectID:   projectID,
		Email:       email,
		UniqueID:    uniqueID,
		DisplayName: req.ServiceAccount.DisplayName,
		CreatedAt:   created,
	}))
}

func (h *Handler) listServiceAccounts(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project")
	resource := "projects/" + projectID
	if _, ok := h.require(w, r, "iam.serviceAccounts.list", resource); !ok {
		return
	}
	list, err := h.Store.ListServiceAccounts(projectID)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	accounts := make([]map[string]any, 0, len(list))
	for _, sa := range list {
		accounts = append(accounts, saJSON(sa))
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": accounts})
}

func (h *Handler) getServiceAccount(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project")
	account := decodeAccount(r.PathValue("account"))
	if strings.Contains(account, ":") {
		gcperrors.InvalidArgument(w, "use POST for service account custom methods")
		return
	}
	resource := "projects/" + projectID
	if _, ok := h.require(w, r, "iam.serviceAccounts.get", resource); !ok {
		return
	}
	sa, ok, err := h.Store.GetServiceAccountInProject(projectID, account)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Service account does not exist.")
		return
	}
	writeJSON(w, http.StatusOK, saJSON(sa))
}

func (h *Handler) patchServiceAccount(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project")
	account := decodeAccount(r.PathValue("account"))
	if strings.Contains(account, ":") {
		gcperrors.InvalidArgument(w, "invalid service account name")
		return
	}
	resource := "projects/" + projectID
	if _, ok := h.require(w, r, "iam.serviceAccounts.update", resource); !ok {
		return
	}
	sa, ok, err := h.Store.GetServiceAccountInProject(projectID, account)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Service account does not exist.")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		gcperrors.InvalidArgument(w, "unable to read body")
		return
	}
	var req struct {
		ServiceAccount struct {
			DisplayName string `json:"displayName"`
		} `json:"serviceAccount"`
		DisplayName string `json:"displayName"`
		UpdateMask  string `json:"updateMask"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	mask := req.UpdateMask
	if mask == "" {
		mask = r.URL.Query().Get("updateMask")
	}
	displayName := req.ServiceAccount.DisplayName
	if displayName == "" {
		displayName = req.DisplayName
	}
	if mask != "" && !fieldMaskIncludes(mask, "displayName") && !fieldMaskIncludes(mask, "serviceAccount.displayName") {
		gcperrors.InvalidArgument(w, "updateMask must include displayName")
		return
	}
	updated, ok, err := h.Store.UpdateServiceAccountDisplayName(sa.Email, displayName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Service account does not exist.")
		return
	}
	writeJSON(w, http.StatusOK, saJSON(updated))
}

func (h *Handler) serviceAccountPost(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project")
	raw := decodeAccount(r.PathValue("account"))
	account, action := splitColonAction(raw)
	if account == "" || action == "" {
		gcperrors.InvalidArgument(w, "expected serviceAccounts/{account}:enable|disable|undelete|signBlob|getIamPolicy|setIamPolicy|testIamPermissions")
		return
	}
	projectResource := "projects/" + projectID

	if action == "undelete" {
		h.undeleteServiceAccount(w, r, projectID, projectResource, account)
		return
	}

	sa, ok, err := h.Store.GetServiceAccountInProject(projectID, account)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Service account does not exist.")
		return
	}
	saResource := fmt.Sprintf("projects/%s/serviceAccounts/%s", projectID, sa.Email)

	switch action {
	case "enable":
		if _, ok := h.require(w, r, "iam.serviceAccounts.enable", projectResource); !ok {
			return
		}
		_, ok, err := h.Store.SetServiceAccountDisabled(sa.Email, false)
		if err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		}
		if !ok {
			gcperrors.NotFound(w, "Service account does not exist.")
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	case "disable":
		if _, ok := h.require(w, r, "iam.serviceAccounts.disable", projectResource); !ok {
			return
		}
		_, ok, err := h.Store.SetServiceAccountDisabled(sa.Email, true)
		if err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		}
		if !ok {
			gcperrors.NotFound(w, "Service account does not exist.")
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	case "signBlob":
		h.signBlob(w, r, projectResource, sa)
	case "getIamPolicy":
		h.getIamPolicy(w, r, projectResource, saResource)
	case "setIamPolicy":
		h.setIamPolicy(w, r, projectResource, saResource)
	case "testIamPermissions":
		h.testIamPermissions(w, r, saResource)
	default:
		gcperrors.InvalidArgument(w, "unknown method on service account")
	}
}

func (h *Handler) undeleteServiceAccount(w http.ResponseWriter, r *http.Request, projectID, projectResource, account string) {
	if _, ok := h.require(w, r, "iam.serviceAccounts.undelete", projectResource); !ok {
		return
	}
	sa, ok, err := h.Store.GetDeletedServiceAccountInProject(projectID, account)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Deleted service account does not exist.")
		return
	}
	restored, ok, err := h.Store.UndeleteServiceAccount(sa.Email)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Deleted service account does not exist.")
		return
	}
	writeJSON(w, http.StatusOK, saJSON(restored))
}

func (h *Handler) signBlob(w http.ResponseWriter, r *http.Request, projectResource string, sa store.ServiceAccount) {
	if _, ok := h.require(w, r, "iam.serviceAccounts.signBlob", projectResource); !ok {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		gcperrors.InvalidArgument(w, "unable to read body")
		return
	}
	var req struct {
		BytesToSign string `json:"bytesToSign"`
		Payload     string `json:"payload"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	b64 := req.BytesToSign
	if b64 == "" {
		b64 = req.Payload
	}
	if b64 == "" {
		gcperrors.InvalidArgument(w, "bytesToSign (or payload) is required")
		return
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		// Accept URL-safe base64 as well.
		raw, err = base64.URLEncoding.DecodeString(b64)
		if err != nil {
			gcperrors.InvalidArgument(w, "bytesToSign must be base64")
			return
		}
	}
	sum := sha256.Sum256(raw)
	sig := base64.StdEncoding.EncodeToString(sum[:])
	writeJSON(w, http.StatusOK, map[string]any{
		"keyId":     "lab-sha256",
		"signature": sig,
		// IAM Credentials API field name (same digest; not an RSA signature).
		"signedBlob": sig,
	})
}

func (h *Handler) getIamPolicy(w http.ResponseWriter, r *http.Request, projectResource, saResource string) {
	if _, ok := h.require(w, r, "iam.serviceAccounts.getIamPolicy", projectResource); !ok {
		return
	}
	raw, ok, err := h.Store.GetIAMPolicyJSON(saResource)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		writeJSON(w, http.StatusOK, authz.Policy{Etag: "ACAB"})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func (h *Handler) setIamPolicy(w http.ResponseWriter, r *http.Request, projectResource, saResource string) {
	if _, ok := h.require(w, r, "iam.serviceAccounts.setIamPolicy", projectResource); !ok {
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		gcperrors.InvalidArgument(w, "unable to read body")
		return
	}
	var req struct {
		Policy authz.Policy `json:"policy"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	if err := h.Store.PutIAMPolicyJSON(saResource, req.Policy); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, req.Policy)
}

func (h *Handler) testIamPermissions(w http.ResponseWriter, r *http.Request, saResource string) {
	p, ok := h.principal(r)
	if !ok {
		gcperrors.Unauthenticated(w, "")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		gcperrors.InvalidArgument(w, "unable to read body")
		return
	}
	var req struct {
		Permissions []string `json:"permissions"`
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			gcperrors.InvalidArgument(w, "invalid JSON body")
			return
		}
	}
	granted, err := h.Authz.TestIamPermissions(p.Email, p.IsRoot, saResource, req.Permissions)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if granted == nil {
		granted = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"permissions": granted})
}

func (h *Handler) deleteServiceAccount(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project")
	account := decodeAccount(r.PathValue("account"))
	resource := "projects/" + projectID
	if _, ok := h.require(w, r, "iam.serviceAccounts.delete", resource); !ok {
		return
	}
	sa, ok, err := h.Store.GetServiceAccountInProject(projectID, account)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Service account does not exist.")
		return
	}
	deleted, err := h.Store.DeleteServiceAccount(sa.Email)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !deleted {
		gcperrors.NotFound(w, "Service account does not exist.")
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("{}"))
}

func (h *Handler) createKey(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project")
	account := decodeAccount(r.PathValue("account"))
	resource := "projects/" + projectID
	if _, ok := h.require(w, r, "iam.serviceAccountKeys.create", resource); !ok {
		return
	}
	sa, ok, err := h.Store.GetServiceAccountInProject(projectID, account)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Service account does not exist.")
		return
	}

	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	var req struct {
		KeyAlgorithm   string `json:"keyAlgorithm"`
		PrivateKeyType string `json:"privateKeyType"`
	}
	if len(body) > 0 {
		_ = json.Unmarshal(body, &req)
	}
	if req.KeyAlgorithm == "" {
		req.KeyAlgorithm = "KEY_ALG_RSA_2048"
	}
	if req.PrivateKeyType == "" {
		req.PrivateKeyType = "TYPE_GOOGLE_CREDENTIALS_FILE"
	}

	keyID := newKeyID()
	accessToken := newAccessToken()
	now := h.now()
	validAfter := now.Format(time.RFC3339)
	validBefore := "9999-12-31T23:59:59Z"
	keyName := fmt.Sprintf("projects/%s/serviceAccounts/%s/keys/%s", projectID, sa.Email, keyID)

	cred := map[string]any{
		"type":                        "service_account",
		"project_id":                  projectID,
		"private_key_id":              keyID,
		"private_key":                 accessToken,
		"client_email":                sa.Email,
		"client_id":                   sa.UniqueID,
		"auth_uri":                    "https://accounts.google.com/o/oauth2/auth",
		"token_uri":                   "https://oauth2.googleapis.com/token",
		"auth_provider_x509_cert_url": "https://www.googleapis.com/oauth2/v1/certs",
		"universe_domain":             "googleapis.com",
	}
	credJSON, err := json.Marshal(cred)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	sealed, err := h.Store.Seal(credJSON)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if err := h.Store.CreateServiceAccountKey(store.ServiceAccountKey{
		Name:            keyName,
		SAEmail:         sa.Email,
		KeyAlgorithm:    req.KeyAlgorithm,
		PrivateKeyType:  req.PrivateKeyType,
		PrivateKeyData:  sealed,
		ValidAfterTime:  validAfter,
		ValidBeforeTime: validBefore,
		CreatedAt:       now.Format(time.RFC3339Nano),
	}); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if err := h.Store.PutAccessToken(authn.HashToken(accessToken), sa.Email, now.Add(365*24*time.Hour)); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"name":            keyName,
		"privateKeyType":  req.PrivateKeyType,
		"keyAlgorithm":    req.KeyAlgorithm,
		"privateKeyData":  base64.StdEncoding.EncodeToString(credJSON),
		"validAfterTime":  validAfter,
		"validBeforeTime": validBefore,
		"keyOrigin":       "GOOGLE_PROVIDED",
		"keyType":         "USER_MANAGED",
	})
}

func (h *Handler) listKeys(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project")
	account := decodeAccount(r.PathValue("account"))
	resource := "projects/" + projectID
	if _, ok := h.require(w, r, "iam.serviceAccountKeys.list", resource); !ok {
		return
	}
	sa, ok, err := h.Store.GetServiceAccountInProject(projectID, account)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Service account does not exist.")
		return
	}
	pageSize := 0
	if v := r.URL.Query().Get("pageSize"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			gcperrors.InvalidArgument(w, "invalid pageSize")
			return
		}
		pageSize = n
	}
	keys, next, err := h.Store.ListServiceAccountKeysPage(sa.Email, pageSize, r.URL.Query().Get("pageToken"))
	if err != nil {
		if strings.Contains(err.Error(), "pageToken") {
			gcperrors.InvalidArgument(w, err.Error())
			return
		}
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		out = append(out, keyMetaJSON(k))
	}
	resp := map[string]any{"keys": out}
	if next != "" {
		resp["nextPageToken"] = next
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) getKey(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project")
	account := decodeAccount(r.PathValue("account"))
	keyID := r.PathValue("key")
	resource := "projects/" + projectID
	if _, ok := h.require(w, r, "iam.serviceAccountKeys.get", resource); !ok {
		return
	}
	sa, ok, err := h.Store.GetServiceAccountInProject(projectID, account)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Service account does not exist.")
		return
	}
	name := fmt.Sprintf("projects/%s/serviceAccounts/%s/keys/%s", projectID, sa.Email, keyID)
	k, ok, err := h.Store.GetServiceAccountKey(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Key does not exist.")
		return
	}
	writeJSON(w, http.StatusOK, keyMetaJSON(k))
}

func (h *Handler) deleteKey(w http.ResponseWriter, r *http.Request) {
	projectID := r.PathValue("project")
	account := decodeAccount(r.PathValue("account"))
	keyID := r.PathValue("key")
	resource := "projects/" + projectID
	if _, ok := h.require(w, r, "iam.serviceAccountKeys.delete", resource); !ok {
		return
	}
	sa, ok, err := h.Store.GetServiceAccountInProject(projectID, account)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Service account does not exist.")
		return
	}
	name := fmt.Sprintf("projects/%s/serviceAccounts/%s/keys/%s", projectID, sa.Email, keyID)
	deleted, err := h.Store.DeleteServiceAccountKey(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !deleted {
		gcperrors.NotFound(w, "Key does not exist.")
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("{}"))
}

func saJSON(sa store.ServiceAccount) map[string]any {
	return map[string]any{
		"name":        fmt.Sprintf("projects/%s/serviceAccounts/%s", sa.ProjectID, sa.Email),
		"projectId":   sa.ProjectID,
		"uniqueId":    sa.UniqueID,
		"email":       sa.Email,
		"displayName": sa.DisplayName,
		"disabled":    sa.Disabled,
	}
}

func keyMetaJSON(k store.ServiceAccountKey) map[string]any {
	return map[string]any{
		"name":            k.Name,
		"keyAlgorithm":    k.KeyAlgorithm,
		"validAfterTime":  k.ValidAfterTime,
		"validBeforeTime": k.ValidBeforeTime,
		"keyOrigin":       "GOOGLE_PROVIDED",
		"keyType":         "USER_MANAGED",
	}
}

func decodeAccount(v string) string {
	if u, err := url.PathUnescape(v); err == nil {
		return u
	}
	return v
}

func splitColonAction(segment string) (id, action string) {
	if i := strings.IndexByte(segment, ':'); i >= 0 {
		return segment[:i], segment[i+1:]
	}
	return segment, ""
}

func fieldMaskIncludes(mask, field string) bool {
	for _, part := range strings.Split(mask, ",") {
		if strings.TrimSpace(part) == field {
			return true
		}
	}
	return false
}

func validAccountID(id string) bool {
	if len(id) < 6 || len(id) > 30 {
		return false
	}
	if id[0] < 'a' || id[0] > 'z' {
		return false
	}
	for i := 1; i < len(id); i++ {
		c := id[i]
		ok := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-'
		if !ok {
			return false
		}
	}
	last := id[len(id)-1]
	return (last >= 'a' && last <= 'z') || (last >= '0' && last <= '9')
}

func newUniqueID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	n := uint64(b[0])<<56 | uint64(b[1])<<48 | uint64(b[2])<<40 | uint64(b[3])<<32 |
		uint64(b[4])<<24 | uint64(b[5])<<16 | uint64(b[6])<<8 | uint64(b[7])
	return fmt.Sprintf("%d", 1_000_000_000_000_000_000+n%8_000_000_000_000_000_000)
}

func newKeyID() string {
	var b [20]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func newAccessToken() string {
	var b [32]byte
	_, _ = rand.Read(b[:])
	return "ngsa_" + hex.EncodeToString(b[:])
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
