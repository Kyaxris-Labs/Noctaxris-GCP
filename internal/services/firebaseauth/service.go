package firebaseauth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// Service serves Identity Toolkit / Firebase Auth REST (lab subset).
type Service struct {
	Store          *store.Store
	Authz          *authz.Evaluator
	DefaultProject string
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// Mount registers Identity Toolkit routes.
// Colon custom methods sit inside the path segment (ServeMux-safe).
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	mux.HandleFunc("POST /identitytoolkit.googleapis.com/v1/accounts:signUp", s.wrapOptional(principalFrom, s.signUp))
	mux.HandleFunc("POST /identitytoolkit.googleapis.com/v1/accounts:signInWithPassword", s.wrapOptional(principalFrom, s.signIn))
	mux.HandleFunc("POST /identitytoolkit.googleapis.com/v1/accounts:lookup", s.wrapOptional(principalFrom, s.lookup))
	mux.HandleFunc("POST /identitytoolkit.googleapis.com/v1/accounts:delete", s.wrapOptional(principalFrom, s.deleteAccount))
	mux.HandleFunc("POST /identitytoolkit.googleapis.com/v1/accounts:update", s.wrapOptional(principalFrom, s.updateAccount))
	mux.HandleFunc("POST /identitytoolkit.googleapis.com/v1/accounts:signInWithCustomToken", s.wrapOptional(principalFrom, s.signInCustomToken))
	mux.HandleFunc("POST /identitytoolkit.googleapis.com/v1/accounts:sendOobCode", s.wrapOptional(principalFrom, s.sendOobCode))
	mux.HandleFunc("POST /identitytoolkit.googleapis.com/v1/accounts:resetPassword", s.wrapOptional(principalFrom, s.resetPassword))

	mux.HandleFunc("POST /identitytoolkit.googleapis.com/v1/projects/{project}/accounts", s.wrap(principalFrom, s.adminCreate))
	mux.HandleFunc("GET /identitytoolkit.googleapis.com/v1/projects/{project}/accounts", s.wrap(principalFrom, s.adminList))
	mux.HandleFunc("GET /identitytoolkit.googleapis.com/v1/projects/{project}/accounts/{localId}", s.wrap(principalFrom, s.adminGet))
	mux.HandleFunc("PATCH /identitytoolkit.googleapis.com/v1/projects/{project}/accounts/{localId}", s.wrap(principalFrom, s.adminPatch))
	mux.HandleFunc("DELETE /identitytoolkit.googleapis.com/v1/projects/{project}/accounts/{localId}", s.wrap(principalFrom, s.adminDelete))
	mux.HandleFunc("POST /identitytoolkit.googleapis.com/v1/projects/{project}/accounts:createCustomToken", s.wrap(principalFrom, s.createCustomToken))
	mux.HandleFunc("POST /identitytoolkit.googleapis.com/v1/projects/{project}/accounts:setCustomUserClaims", s.wrap(principalFrom, s.setCustomUserClaims))
	mux.HandleFunc("POST /identitytoolkit.googleapis.com/v1/projects/{project}/accounts:verifyIdToken", s.wrap(principalFrom, s.verifyIdToken))
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

func (s *Service) wrapOptional(principalFrom principalFunc, h handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := principalFrom(r)
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

func (s *Service) projectFromBody(r *http.Request, bodyProject string) string {
	if bodyProject != "" {
		return bodyProject
	}
	if q := r.URL.Query().Get("key"); strings.HasPrefix(q, "project:") {
		return strings.TrimPrefix(q, "project:")
	}
	if s.DefaultProject != "" {
		return s.DefaultProject
	}
	return "noctaxris-gcp-local"
}

func hashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.MinCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func checkPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

func userRecord(u *store.FirebaseUser, idToken string) map[string]any {
	out := map[string]any{
		"localId":          u.LocalID,
		"email":            u.Email,
		"displayName":      u.DisplayName,
		"disabled":         u.Disabled,
		"emailVerified":    true,
		"customAttributes": u.CustomAttributes,
		"createdAt":        u.CreatedAt,
	}
	if idToken != "" {
		out["idToken"] = idToken
		out["refreshToken"] = "lab-refresh-" + u.LocalID
		out["expiresIn"] = "3600"
	}
	return out
}

func mintIDToken(u *store.FirebaseUser) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	claims := map[string]any{
		"user_id":     u.LocalID,
		"sub":         u.LocalID,
		"email":       u.Email,
		"firebase":    map[string]any{"sign_in_provider": "password"},
		"iat":         time.Now().Unix(),
		"exp":         time.Now().Add(time.Hour).Unix(),
		"aud":         u.ProjectID,
		"iss":         "https://securetoken.google.com/" + u.ProjectID,
	}
	if u.CustomAttributes != "" && u.CustomAttributes != "{}" {
		var custom map[string]any
		if err := json.Unmarshal([]byte(u.CustomAttributes), &custom); err == nil {
			for k, v := range custom {
				claims[k] = v
			}
		}
	}
	raw, _ := json.Marshal(claims)
	payload := base64.RawURLEncoding.EncodeToString(raw)
	// Unsigned lab JWT (empty signature segment).
	return header + "." + payload + "."
}

func parseLabJWT(token string) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid token")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var claims map[string]any
	if err := json.Unmarshal(raw, &claims); err != nil {
		return nil, err
	}
	return claims, nil
}

// uidFromClaims returns user_id/sub from lab JWT claims (same fields as verifyIdToken).
func uidFromClaims(claims map[string]any) string {
	uid, _ := claims["user_id"].(string)
	if uid == "" {
		uid, _ = claims["sub"].(string)
	}
	return uid
}

// uidFromLabIDToken returns user_id/sub from an unsigned lab idToken (same path as verifyIdToken).
func uidFromLabIDToken(idToken string) (string, error) {
	claims, err := parseLabJWT(idToken)
	if err != nil {
		return "", err
	}
	uid := uidFromClaims(claims)
	if uid == "" {
		return "", fmt.Errorf("idToken missing sub")
	}
	return uid, nil
}

// requireClientIDToken enforces Identity Toolkit client delete/update auth:
// missing idToken → 401 MISSING_ID_TOKEN; invalid or localId mismatch → 400 INVALID_ID_TOKEN.
// Returns the uid from the token (localID may be empty and is then taken from the token).
func requireClientIDToken(w http.ResponseWriter, idToken, localID string) (uid string, ok bool) {
	if idToken == "" {
		gcperrors.Unauthenticated(w, "MISSING_ID_TOKEN")
		return "", false
	}
	uid, err := uidFromLabIDToken(idToken)
	if err != nil {
		gcperrors.InvalidArgument(w, "INVALID_ID_TOKEN")
		return "", false
	}
	if localID != "" && localID != uid {
		gcperrors.InvalidArgument(w, "INVALID_ID_TOKEN")
		return "", false
	}
	return uid, true
}

func mintCustomToken(projectID, uid string, claims map[string]any) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	body := map[string]any{
		"uid": uid,
		"sub": uid,
		"iss": "noctaxris-gcp-lab@" + projectID + ".iam.gserviceaccount.com",
		"aud": "https://identitytoolkit.googleapis.com/google.identity.identitytoolkit.v1.IdentityToolkit",
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	if len(claims) > 0 {
		body["claims"] = claims
	}
	raw, _ := json.Marshal(body)
	payload := base64.RawURLEncoding.EncodeToString(raw)
	return header + "." + payload + "."
}

func (s *Service) signUp(w http.ResponseWriter, r *http.Request, _ authn.Principal) {
	var body struct {
		Email             string `json:"email"`
		Password          string `json:"password"`
		DisplayName       string `json:"displayName"`
		ReturnSecureToken bool   `json:"returnSecureToken"`
		TargetProjectID   string `json:"targetProjectId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	if body.Email == "" || body.Password == "" {
		gcperrors.InvalidArgument(w, "email and password are required")
		return
	}
	project := s.projectFromBody(r, body.TargetProjectID)
	hash, err := hashPassword(body.Password)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	u, created, err := s.Store.CreateFirebaseUser(store.FirebaseUser{
		LocalID: uuid.NewString(), ProjectID: project, Email: body.Email,
		PasswordHash: hash, DisplayName: body.DisplayName,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusBadRequest, gcperrors.StatusAlreadyExists, "EMAIL_EXISTS")
		return
	}
	token := ""
	if body.ReturnSecureToken {
		token = mintIDToken(u)
	}
	writeJSON(w, http.StatusOK, userRecord(u, token))
}

func (s *Service) signIn(w http.ResponseWriter, r *http.Request, _ authn.Principal) {
	var body struct {
		Email             string `json:"email"`
		Password          string `json:"password"`
		ReturnSecureToken bool   `json:"returnSecureToken"`
		TargetProjectID   string `json:"targetProjectId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	project := s.projectFromBody(r, body.TargetProjectID)
	u, ok, err := s.Store.GetFirebaseUserByEmail(project, body.Email)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok || !checkPassword(u.PasswordHash, body.Password) || u.Disabled {
		gcperrors.WriteREST(w, http.StatusBadRequest, gcperrors.StatusInvalidArgument, "INVALID_LOGIN_CREDENTIALS")
		return
	}
	token := ""
	if body.ReturnSecureToken {
		token = mintIDToken(u)
	}
	writeJSON(w, http.StatusOK, userRecord(u, token))
}

func (s *Service) lookup(w http.ResponseWriter, r *http.Request, _ authn.Principal) {
	var body struct {
		LocalID         []string `json:"localId"`
		Email           []string `json:"email"`
		IDToken         string   `json:"idToken"`
		TargetProjectID string   `json:"targetProjectId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	project := s.projectFromBody(r, body.TargetProjectID)
	users := []map[string]any{}
	if body.IDToken != "" {
		claims, err := parseLabJWT(body.IDToken)
		if err != nil {
			gcperrors.InvalidArgument(w, "invalid idToken")
			return
		}
		uid, _ := claims["user_id"].(string)
		if uid == "" {
			uid, _ = claims["sub"].(string)
		}
		if uid != "" {
			u, ok, err := s.Store.GetFirebaseUserByLocalID(uid)
			if err != nil {
				gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
				return
			}
			if ok && u.ProjectID == project {
				users = append(users, userRecord(u, ""))
			}
		}
	}
	for _, id := range body.LocalID {
		u, ok, err := s.Store.GetFirebaseUserByLocalID(id)
		if err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		}
		if ok && u.ProjectID == project {
			users = append(users, userRecord(u, ""))
		}
	}
	for _, email := range body.Email {
		u, ok, err := s.Store.GetFirebaseUserByEmail(project, email)
		if err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		}
		if ok {
			users = append(users, userRecord(u, ""))
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": users})
}

func (s *Service) sendOobCode(w http.ResponseWriter, r *http.Request, _ authn.Principal) {
	var body struct {
		RequestType     string `json:"requestType"`
		Email           string `json:"email"`
		TargetProjectID string `json:"targetProjectId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	if body.RequestType != "PASSWORD_RESET" {
		gcperrors.InvalidArgument(w, "supported requestType: PASSWORD_RESET")
		return
	}
	if body.Email == "" {
		gcperrors.InvalidArgument(w, "email is required")
		return
	}
	project := s.projectFromBody(r, body.TargetProjectID)
	u, ok, err := s.Store.GetFirebaseUserByEmail(project, body.Email)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.WriteREST(w, http.StatusBadRequest, gcperrors.StatusInvalidArgument, "EMAIL_NOT_FOUND")
		return
	}
	code := "lab-oob-" + uuid.NewString()
	if err := s.Store.CreateFirebaseOOBCode(store.FirebaseOOBCode{
		OOBCode: code, ProjectID: project, Email: body.Email, RequestType: body.RequestType, LocalID: u.LocalID,
	}); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"email":       body.Email,
		"requestType": body.RequestType,
		"oobCode":     code, // lab theatre: returned so tests can complete reset without mail
	})
}

func (s *Service) resetPassword(w http.ResponseWriter, r *http.Request, _ authn.Principal) {
	var body struct {
		OOBCode     string `json:"oobCode"`
		NewPassword string `json:"newPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	if body.OOBCode == "" || body.NewPassword == "" {
		gcperrors.InvalidArgument(w, "oobCode and newPassword are required")
		return
	}
	c, ok, err := s.Store.ConsumeFirebaseOOBCode(body.OOBCode)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.WriteREST(w, http.StatusBadRequest, gcperrors.StatusInvalidArgument, "INVALID_OOB_CODE")
		return
	}
	u, ok, err := s.Store.GetFirebaseUserByLocalID(c.LocalID)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "user not found")
		return
	}
	hash, err := hashPassword(body.NewPassword)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	u.PasswordHash = hash
	if err := s.Store.UpdateFirebaseUser(*u); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"email":   u.Email,
		"requestType": "PASSWORD_RESET",
	})
}

func (s *Service) deleteAccount(w http.ResponseWriter, r *http.Request, _ authn.Principal) {
	var body struct {
		LocalID string `json:"localId"`
		IDToken string `json:"idToken"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	uid, ok := requireClientIDToken(w, body.IDToken, body.LocalID)
	if !ok {
		return
	}
	deleted, err := s.Store.DeleteFirebaseUser(uid)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !deleted {
		gcperrors.NotFound(w, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

func (s *Service) updateAccount(w http.ResponseWriter, r *http.Request, _ authn.Principal) {
	var body struct {
		LocalID     string `json:"localId"`
		IDToken     string `json:"idToken"`
		DisplayName string `json:"displayName"`
		Password    string `json:"password"`
		DisableUser *bool  `json:"disableUser"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	uid, ok := requireClientIDToken(w, body.IDToken, body.LocalID)
	if !ok {
		return
	}
	u, ok, err := s.Store.GetFirebaseUserByLocalID(uid)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "user not found")
		return
	}
	if body.DisplayName != "" {
		u.DisplayName = body.DisplayName
	}
	if body.DisableUser != nil {
		u.Disabled = *body.DisableUser
	}
	if body.Password != "" {
		hash, err := hashPassword(body.Password)
		if err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		}
		u.PasswordHash = hash
	}
	if err := s.Store.UpdateFirebaseUser(*u); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, userRecord(u, mintIDToken(u)))
}

func (s *Service) signInCustomToken(w http.ResponseWriter, r *http.Request, _ authn.Principal) {
	var body struct {
		Token             string `json:"token"`
		ReturnSecureToken bool   `json:"returnSecureToken"`
		TargetProjectID   string `json:"targetProjectId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	parts := strings.Split(body.Token, ".")
	if len(parts) < 2 {
		gcperrors.InvalidArgument(w, "invalid custom token")
		return
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		gcperrors.InvalidArgument(w, "invalid custom token payload")
		return
	}
	var claims struct {
		UID string `json:"uid"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil || claims.UID == "" {
		gcperrors.InvalidArgument(w, "custom token missing uid")
		return
	}
	project := s.projectFromBody(r, body.TargetProjectID)
	u, ok, err := s.Store.GetFirebaseUserByLocalID(claims.UID)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		// Auto-provision lab user for custom token uid.
		u, _, err = s.Store.CreateFirebaseUser(store.FirebaseUser{
			LocalID: claims.UID, ProjectID: project,
			Email: claims.UID + "@lab.invalid", PasswordHash: "!",
		})
		if err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		}
	}
	token := ""
	if body.ReturnSecureToken {
		token = mintIDToken(u)
	}
	writeJSON(w, http.StatusOK, userRecord(u, token))
}

func (s *Service) adminCreate(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "firebaseauth.users.create", project); err != nil {
		writeAuthz(w, err)
		return
	}
	var body struct {
		Email       string `json:"email"`
		Password    string `json:"password"`
		DisplayName string `json:"displayName"`
		LocalID     string `json:"localId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	hash, err := hashPassword(body.Password)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	u, created, err := s.Store.CreateFirebaseUser(store.FirebaseUser{
		LocalID: body.LocalID, ProjectID: project, Email: body.Email,
		PasswordHash: hash, DisplayName: body.DisplayName,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "user already exists")
		return
	}
	writeJSON(w, http.StatusOK, userRecord(u, ""))
}

func (s *Service) adminList(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "firebaseauth.users.list", project); err != nil {
		writeAuthz(w, err)
		return
	}
	pageSize := 0
	if n := r.URL.Query().Get("maxResults"); n != "" {
		fmt.Sscanf(n, "%d", &pageSize)
	}
	pageToken := r.URL.Query().Get("nextPageToken")
	if pageToken == "" {
		pageToken = r.URL.Query().Get("pageToken")
	}
	list, next, err := s.Store.ListFirebaseUsersPage(project, pageSize, pageToken)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	users := make([]map[string]any, 0, len(list))
	for i := range list {
		users = append(users, userRecord(&list[i], ""))
	}
	out := map[string]any{"users": users}
	if next != "" {
		out["nextPageToken"] = next
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Service) adminGet(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project, localID := r.PathValue("project"), r.PathValue("localId")
	if err := s.require(p, "firebaseauth.users.get", project); err != nil {
		writeAuthz(w, err)
		return
	}
	u, ok, err := s.Store.GetFirebaseUserByLocalID(localID)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok || u.ProjectID != project {
		gcperrors.NotFound(w, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, userRecord(u, ""))
}

func (s *Service) adminPatch(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project, localID := r.PathValue("project"), r.PathValue("localId")
	if err := s.require(p, "firebaseauth.users.update", project); err != nil {
		writeAuthz(w, err)
		return
	}
	u, ok, err := s.Store.GetFirebaseUserByLocalID(localID)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok || u.ProjectID != project {
		gcperrors.NotFound(w, "user not found")
		return
	}
	var body struct {
		DisplayName string `json:"displayName"`
		Disabled    *bool  `json:"disabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	if body.DisplayName != "" {
		u.DisplayName = body.DisplayName
	}
	if body.Disabled != nil {
		u.Disabled = *body.Disabled
	}
	if err := s.Store.UpdateFirebaseUser(*u); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, userRecord(u, ""))
}

func (s *Service) adminDelete(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project, localID := r.PathValue("project"), r.PathValue("localId")
	if err := s.require(p, "firebaseauth.users.delete", project); err != nil {
		writeAuthz(w, err)
		return
	}
	u, ok, err := s.Store.GetFirebaseUserByLocalID(localID)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok || u.ProjectID != project {
		gcperrors.NotFound(w, "user not found")
		return
	}
	if _, err := s.Store.DeleteFirebaseUser(localID); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) createCustomToken(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "firebaseauth.users.create", project); err != nil {
		writeAuthz(w, err)
		return
	}
	var body struct {
		UID    string         `json:"uid"`
		Claims map[string]any `json:"claims"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	if body.UID == "" {
		gcperrors.InvalidArgument(w, "uid is required")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token": mintCustomToken(project, body.UID, body.Claims),
	})
}

func (s *Service) setCustomUserClaims(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "firebaseauth.users.update", project); err != nil {
		writeAuthz(w, err)
		return
	}
	var body struct {
		LocalID          string         `json:"localId"`
		CustomAttributes map[string]any `json:"customAttributes"`
		Claims           map[string]any `json:"claims"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	if body.LocalID == "" {
		gcperrors.InvalidArgument(w, "localId is required")
		return
	}
	u, ok, err := s.Store.GetFirebaseUserByLocalID(body.LocalID)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok || u.ProjectID != project {
		gcperrors.NotFound(w, "user not found")
		return
	}
	claims := body.CustomAttributes
	if claims == nil {
		claims = body.Claims
	}
	if claims == nil {
		claims = map[string]any{}
	}
	raw, _ := json.Marshal(claims)
	u.CustomAttributes = string(raw)
	if err := s.Store.UpdateFirebaseUser(*u); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, userRecord(u, ""))
}

func (s *Service) verifyIdToken(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	if err := s.require(p, "firebaseauth.users.get", project); err != nil {
		writeAuthz(w, err)
		return
	}
	var body struct {
		IDToken string `json:"idToken"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	if body.IDToken == "" {
		gcperrors.InvalidArgument(w, "idToken is required")
		return
	}
	claims, err := parseLabJWT(body.IDToken)
	if err != nil {
		gcperrors.InvalidArgument(w, "invalid idToken")
		return
	}
	uid := uidFromClaims(claims)
	if uid == "" {
		gcperrors.InvalidArgument(w, "idToken missing sub")
		return
	}
	u, ok, err := s.Store.GetFirebaseUserByLocalID(uid)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok || u.ProjectID != project {
		gcperrors.NotFound(w, "user not found")
		return
	}
	if aud, _ := claims["aud"].(string); aud != "" && aud != project {
		gcperrors.InvalidArgument(w, "idToken audience mismatch")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"uid":    u.LocalID,
		"email":  u.Email,
		"claims": claims,
		"valid":  true,
	})
}

func writeAuthz(w http.ResponseWriter, err error) {
	if err == errDenied {
		gcperrors.PermissionDenied(w, "")
		return
	}
	gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
}
