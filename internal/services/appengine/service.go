package appengine

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// Service serves App Engine Admin API v1 REST (control-plane theatre; no runtime).
type Service struct {
	Store *store.Store
	Authz *authz.Evaluator
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// Mount registers App Engine v1 apps/services/versions routes.
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	mux.HandleFunc("POST /v1/apps", s.wrap(principalFrom, s.createApp))
	mux.HandleFunc("GET /v1/apps/{app}", s.wrap(principalFrom, s.getApp))
	mux.HandleFunc("GET /v1/apps/{app}/services", s.wrap(principalFrom, s.listServices))
	mux.HandleFunc("GET /v1/apps/{app}/services/{service}", s.wrap(principalFrom, s.getService))
	mux.HandleFunc("PATCH /v1/apps/{app}/services/{service}", s.wrap(principalFrom, s.patchService))
	mux.HandleFunc("GET /v1/apps/{app}/services/{service}/versions", s.wrap(principalFrom, s.listVersions))
	mux.HandleFunc("POST /v1/apps/{app}/services/{service}/versions", s.wrap(principalFrom, s.createVersion))
	mux.HandleFunc("GET /v1/apps/{app}/services/{service}/versions/{version}", s.wrap(principalFrom, s.getVersion))
	mux.HandleFunc("DELETE /v1/apps/{app}/services/{service}/versions/{version}", s.wrap(principalFrom, s.deleteVersion))
	mux.HandleFunc("GET /v1/apps/{app}/services/{service}/versions/{version}/instances", s.wrap(principalFrom, s.listInstances))
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

var errDenied = fmt.Errorf("permission denied")

func (s *Service) require(p authn.Principal, permission, appID string) error {
	// Application id equals the GCP project id; evaluate on the project resource
	// so seeded roles/owner bindings apply (lab inheritance).
	ok, err := s.Authz.Evaluate(p.Email, p.IsRoot, permission, "projects/"+appID)
	if err != nil {
		return err
	}
	if !ok {
		return errDenied
	}
	return nil
}

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

func (s *Service) createApp(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		gcperrors.InvalidArgument(w, "unable to read body")
		return
	}
	var req struct {
		ID         string `json:"id"`
		LocationID string `json:"locationId"`
		AuthDomain string `json:"authDomain"`
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			gcperrors.InvalidArgument(w, "invalid JSON body")
			return
		}
	}
	if req.ID == "" {
		gcperrors.InvalidArgument(w, "id is required")
		return
	}
	if err := s.require(p, "appengine.applications.create", req.ID); err != nil {
		writeAuthzErr(w, err)
		return
	}
	created, err := s.Store.CreateAppEngineApp(store.AppEngineApp{
		AppID:      req.ID,
		LocationID: req.LocationID,
		AuthDomain: req.AuthDomain,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "application already exists")
		return
	}
	app, ok, err := s.Store.GetAppEngineApp(req.ID)
	if err != nil || !ok {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "created application missing")
		return
	}
	// Lab returns Application synchronously (GCP returns an LRO Operation).
	writeJSON(w, http.StatusOK, toAppJSON(app))
}

func (s *Service) getApp(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	appID := r.PathValue("app")
	if appID == "" || strings.Contains(appID, ":") {
		gcperrors.InvalidArgument(w, "invalid app id")
		return
	}
	if err := s.require(p, "appengine.applications.get", appID); err != nil {
		writeAuthzErr(w, err)
		return
	}
	app, ok, err := s.Store.GetAppEngineApp(appID)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Application not found")
		return
	}
	writeJSON(w, http.StatusOK, toAppJSON(app))
}

func (s *Service) listServices(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	appID := r.PathValue("app")
	if err := s.require(p, "appengine.services.list", appID); err != nil {
		writeAuthzErr(w, err)
		return
	}
	if _, ok, err := s.Store.GetAppEngineApp(appID); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "Application not found")
		return
	}
	list, err := s.Store.ListAppEngineServices(appID)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, svc := range list {
		items = append(items, toServiceJSON(svc))
	}
	writeJSON(w, http.StatusOK, map[string]any{"services": items})
}

func (s *Service) getService(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	appID := r.PathValue("app")
	serviceID := r.PathValue("service")
	if err := s.require(p, "appengine.services.get", appID); err != nil {
		writeAuthzErr(w, err)
		return
	}
	svc, ok, err := s.Store.GetAppEngineService(appID, serviceID)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Service not found")
		return
	}
	writeJSON(w, http.StatusOK, toServiceJSON(svc))
}

func (s *Service) patchService(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	appID := r.PathValue("app")
	serviceID := r.PathValue("service")
	if err := s.require(p, "appengine.services.update", appID); err != nil {
		writeAuthzErr(w, err)
		return
	}
	if _, ok, err := s.Store.GetAppEngineService(appID, serviceID); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "Service not found")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		gcperrors.InvalidArgument(w, "unable to read body")
		return
	}
	var req struct {
		Split   map[string]any `json:"split"`
		ShardBy string         `json:"shardBy"`
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			gcperrors.InvalidArgument(w, "invalid JSON body")
			return
		}
	}
	migrateTraffic := strings.EqualFold(r.URL.Query().Get("migrateTraffic"), "true")
	splitJSON := "{}"
	if req.Split != nil {
		raw, _ := json.Marshal(req.Split)
		splitJSON = string(raw)
	}
	svc, ok, err := s.Store.UpdateAppEngineServiceTraffic(appID, serviceID, splitJSON, req.ShardBy, migrateTraffic)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "Service not found")
		return
	}
	// Lab returns Service synchronously (GCP returns an LRO Operation).
	writeJSON(w, http.StatusOK, toServiceJSON(svc))
}

func (s *Service) listInstances(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	appID := r.PathValue("app")
	serviceID := r.PathValue("service")
	versionID := r.PathValue("version")
	if err := s.require(p, "appengine.instances.list", appID); err != nil {
		writeAuthzErr(w, err)
		return
	}
	if _, ok, err := s.Store.GetAppEngineVersion(appID, serviceID, versionID); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "Version not found")
		return
	}
	// No runtime / no DinD: empty instance list.
	writeJSON(w, http.StatusOK, map[string]any{"instances": []any{}})
}

func (s *Service) listVersions(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	appID := r.PathValue("app")
	serviceID := r.PathValue("service")
	if err := s.require(p, "appengine.versions.list", appID); err != nil {
		writeAuthzErr(w, err)
		return
	}
	if _, ok, err := s.Store.GetAppEngineService(appID, serviceID); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "Service not found")
		return
	}
	list, err := s.Store.ListAppEngineVersions(appID, serviceID)
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

func (s *Service) createVersion(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	appID := r.PathValue("app")
	serviceID := r.PathValue("service")
	if err := s.require(p, "appengine.versions.create", appID); err != nil {
		writeAuthzErr(w, err)
		return
	}
	if _, ok, err := s.Store.GetAppEngineApp(appID); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "Application not found")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		gcperrors.InvalidArgument(w, "unable to read body")
		return
	}
	var req struct {
		ID           string            `json:"id"`
		Runtime      string            `json:"runtime"`
		Env          string            `json:"env"`
		EnvVariables map[string]string `json:"envVariables"`
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			gcperrors.InvalidArgument(w, "invalid JSON body")
			return
		}
	}
	if req.ID == "" {
		gcperrors.InvalidArgument(w, "id is required")
		return
	}
	envJSON := "{}"
	if req.EnvVariables != nil {
		raw, _ := json.Marshal(req.EnvVariables)
		envJSON = string(raw)
	}
	created, err := s.Store.CreateAppEngineVersion(store.AppEngineVersion{
		AppID:            appID,
		ServiceID:        serviceID,
		VersionID:        req.ID,
		Runtime:          req.Runtime,
		Env:              req.Env,
		EnvVariablesJSON: envJSON,
		CreatedAt:        time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "version already exists")
		return
	}
	v, ok, err := s.Store.GetAppEngineVersion(appID, serviceID, req.ID)
	if err != nil || !ok {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "created version missing")
		return
	}
	writeJSON(w, http.StatusOK, toVersionJSON(v))
}

func (s *Service) getVersion(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	appID := r.PathValue("app")
	serviceID := r.PathValue("service")
	versionID := r.PathValue("version")
	if err := s.require(p, "appengine.versions.get", appID); err != nil {
		writeAuthzErr(w, err)
		return
	}
	v, ok, err := s.Store.GetAppEngineVersion(appID, serviceID, versionID)
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
	appID := r.PathValue("app")
	serviceID := r.PathValue("service")
	versionID := r.PathValue("version")
	if err := s.require(p, "appengine.versions.delete", appID); err != nil {
		writeAuthzErr(w, err)
		return
	}
	ok, err := s.Store.DeleteAppEngineVersion(appID, serviceID, versionID)
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

func toAppJSON(app store.AppEngineApp) map[string]any {
	return map[string]any{
		"name":          app.Name,
		"id":            app.AppID,
		"locationId":    app.LocationID,
		"servingStatus": app.ServingStatus,
		"authDomain":    app.AuthDomain,
		"defaultHostname": app.AppID + ".appspot.local",
		"codeBucket":    "staging." + app.AppID + ".appspot.local",
		"defaultBucket": app.AppID + ".appspot.local",
	}
}

func toServiceJSON(svc store.AppEngineService) map[string]any {
	var split any = map[string]any{"allocations": map[string]any{}}
	if svc.SplitJSON != "" && svc.SplitJSON != "{}" {
		_ = json.Unmarshal([]byte(svc.SplitJSON), &split)
	}
	out := map[string]any{
		"name":    svc.Name,
		"id":      svc.ServiceID,
		"split":   split,
		"shardBy": svc.ShardBy,
	}
	return out
}

func toVersionJSON(v store.AppEngineVersion) map[string]any {
	var envVars any
	_ = json.Unmarshal([]byte(v.EnvVariablesJSON), &envVars)
	if envVars == nil {
		envVars = map[string]string{}
	}
	return map[string]any{
		"name":          v.Name,
		"id":            v.VersionID,
		"runtime":       v.Runtime,
		"env":           v.Env,
		"envVariables":  envVars,
		"servingStatus": v.ServingStatus,
		"createTime":    v.CreatedAt,
		"versionUrl":    fmt.Sprintf("https://%s-dot-%s-dot-%s.appspot.local", v.VersionID, v.ServiceID, v.AppID),
	}
}
